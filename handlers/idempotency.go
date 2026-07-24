package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/gomodule/redigo/redis"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Idempotency Handler middleware
// ===========================================================================

type IdempotencyStoreType int

const (
	IdempotencyStoreTypeLocal IdempotencyStoreType = iota
	IdempotencyStoreTypeShared
	IdempotencyStoreTypeRedis

	idempotencyResponseSaveAttempts = 3
	idempotencyResponseSaveBackoff  = 10 * time.Millisecond
)

func (ist IdempotencyStoreType) String() string {
	return [...]string{"local", "shared", "redis"}[ist]
}

type IdempotencyHandlerOptions struct {
	IgnorePaths []string
	Expiry      time.Duration
}

type IdempotencyRecord struct {
	Key         string    `json:"key" gorm:"column:key;primary_key"`
	ExpiryDate  time.Time `json:"expiryDate" gorm:"column:expiry_date"`
	StatusCode  int       `json:"statusCode" gorm:"column:status_code"`
	ContentType string    `json:"contentType" gorm:"column:content_type"`
	Body        []byte    `json:"body" gorm:"column:body"`
	Completed   bool      `json:"completed" gorm:"column:completed"`
}

type IdempotencyStore interface {
	TryReserve(key string, expiry time.Duration) (bool, *IdempotencyRecord, error)
	SetResponse(key string, record IdempotencyRecord, expiry time.Duration) error
	Release(key string) error
}

// Redis store for idempotency keys
type IdempotencyStoreRedis struct {
	conn   redis.Conn
	prefix string
	mu     sync.Mutex
}

func NewIdempotencyStoreRedis(c redis.Conn) *IdempotencyStoreRedis {
	return &IdempotencyStoreRedis{conn: c, prefix: "idempotencykey"}
}

func (r *IdempotencyStoreRedis) prefixedKey(key string) string {
	return fmt.Sprintf("%s:%s", r.prefix, key)
}

func (r *IdempotencyStoreRedis) Get(key string) (*IdempotencyRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.get(key)
}

func (r *IdempotencyStoreRedis) get(key string) (*IdempotencyRecord, bool, error) {
	raw, err := redis.Bytes(r.conn.Do("GET", r.prefixedKey(key)))
	if errors.Is(err, redis.ErrNil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var record IdempotencyRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return &IdempotencyRecord{Key: key}, true, nil
	}

	return &record, true, nil
}

func (r *IdempotencyStoreRedis) TryReserve(key string, expiry time.Duration) (bool, *IdempotencyRecord, error) {
	record := IdempotencyRecord{Key: key, ExpiryDate: time.Now().Add(expiry)}
	raw, err := json.Marshal(record)
	if err != nil {
		return false, nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	res, err := r.conn.Do("SET", r.prefixedKey(key), raw, "PX", int(expiry.Milliseconds()), "NX")
	if err != nil {
		return false, nil, err
	}

	if res == nil {
		existing, exists, err := r.get(key)
		if err != nil {
			return false, nil, err
		}
		if !exists {
			return false, nil, fmt.Errorf("idempotency key disappeared after reservation conflict")
		}
		return false, existing, nil
	}

	status, err := redis.String(res, nil)
	if err != nil {
		return false, nil, err
	}
	if status != "OK" {
		return false, nil, fmt.Errorf("failed to reserve key: %s", status)
	}

	return true, nil, nil
}

func (r *IdempotencyStoreRedis) SetResponse(key string, record IdempotencyRecord, expiry time.Duration) error {
	record.Key = key
	record.ExpiryDate = time.Now().Add(expiry)
	record.Completed = true

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.setRecord(key, record, expiry)
}

func (r *IdempotencyStoreRedis) Release(key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, err := r.conn.Do("DEL", r.prefixedKey(key))
	return err
}

func (r *IdempotencyStoreRedis) setRecord(key string, record IdempotencyRecord, expiry time.Duration) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}

	res, err := r.conn.Do("PSETEX", r.prefixedKey(key), int(expiry.Milliseconds()), raw)
	if err != nil {
		return err
	}

	if res != "OK" {
		return fmt.Errorf("failed to set key: %v", res)
	}

	return nil
}

// Gorm (SQL) store for idempotency keys
type IdempotencyStoreGorm struct {
	db *gorm.DB
}

func (IdempotencyRecord) TableName() string {
	return "idempotency_keys"
}

func NewIdempotencyStoreGorm(db *gorm.DB) *IdempotencyStoreGorm {
	return &IdempotencyStoreGorm{db: db}
}

func (g *IdempotencyStoreGorm) Get(key string) (*IdempotencyRecord, bool, error) {
	item := IdempotencyRecord{}
	err := g.db.First(&item, "key = ? and expiry_date > ?", key, time.Now()).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// key doesn't exist
		return nil, false, nil
	} else if err != nil {
		// some other error
		return nil, false, err
	}

	// key exists
	return &item, true, nil
}

// TryReserve atomically claims an idempotency key for the caller. It uses a
// single SQL statement via a CTE so that the expired-row deletion and the
// conditional insert happen as one logical step:
//
//	WITH expired AS (
//	  DELETE FROM idempotency_keys
//	  WHERE key = $1 AND expiry_date <= NOW()
//	  RETURNING NULL
//	)
//	INSERT INTO idempotency_keys (key, expiry_date, status_code, content_type, body, completed)
//	SELECT $1, NOW() + $2, 0, '', '', false
//	WHERE NOT EXISTS (SELECT 1 FROM idempotency_keys WHERE key = $1)
//	ON CONFLICT (key) DO NOTHING
//	RETURNING key
//
// Ownership is decided by whether RETURNING produced a row, not by
// RowsAffected: RowsAffected on a CTE + ON CONFLICT DO NOTHING statement is
// not reliably reported as 0/1 per caller through database/sql (observed
// experimentally: under 16 concurrent callers for the same key, more than
// one goroutine saw RowsAffected==1 even though raw psql against the same
// statement always produced exactly one physical row). Scanning the
// RETURNING clause instead reports the ground truth directly from Postgres:
// if a row comes back, this call's INSERT is the one that actually landed;
// sql.ErrNoRows means ON CONFLICT DO NOTHING suppressed it, i.e. the caller
// lost the race (or the row already existed and is still valid). Doing the
// DELETE and the INSERT inside one statement closes the TOCTOU window where
// one transaction's deleted-but-not-committed row could otherwise be
// resurrected by a concurrent transaction's DELETE. The WHERE NOT EXISTS
// guard alone does not stop two concurrent transactions from both
// attempting the INSERT before either commits (Postgres only evaluates NOT
// EXISTS against already-committed rows); ON CONFLICT DO NOTHING is what
// actually prevents the unique-violation error for the loser.
func (g *IdempotencyStoreGorm) TryReserve(key string, expiry time.Duration) (bool, *IdempotencyRecord, error) {
	now := time.Now()

	reserved := false
	err := g.db.Transaction(func(tx *gorm.DB) error {
		// One atomic statement: drop expired entries, then insert-if-absent.
		row := tx.Raw(`
			WITH expired AS (
				DELETE FROM idempotency_keys
				WHERE key = ? AND expiry_date <= ?
			)
			INSERT INTO idempotency_keys (key, expiry_date, status_code, content_type, body, completed)
			SELECT ?, ?, 0, '', '', false
			WHERE NOT EXISTS (SELECT 1 FROM idempotency_keys WHERE key = ?)
			ON CONFLICT (key) DO NOTHING
			RETURNING key
		`, key, now, key, now.Add(expiry), key).Row()

		var returnedKey string
		switch err := row.Scan(&returnedKey); {
		case err == nil:
			reserved = true
			return nil
		case errors.Is(err, sql.ErrNoRows):
			reserved = false
			return nil
		default:
			return err
		}
	})
	if err != nil {
		return false, nil, err
	}
	if reserved {
		return true, nil, nil
	}

	existing, _, getErr := g.Get(key)
	return false, existing, getErr
}

func (g *IdempotencyStoreGorm) SetResponse(
	key string,
	record IdempotencyRecord,
	expiry time.Duration,
) error {
	record.Key = key
	record.ExpiryDate = time.Now().Add(expiry)
	record.Completed = true

	return g.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"expiry_date",
			"status_code",
			"content_type",
			"body",
			"completed",
		}),
	}).Create(&record).Error
}

func (g *IdempotencyStoreGorm) Release(key string) error {
	return g.db.Where("key = ?", key).Delete(&IdempotencyRecord{}).Error
}

// Prune deletes all expired IdempotencyStoreGormItems from the database
func (g *IdempotencyStoreGorm) Prune() error {
	err := g.db.Delete(IdempotencyRecord{}, "expiry_date < ?", time.Now()).Error
	return err
}

// Local / in-memory store for idempotency keys, mainly for testing purposes
type IdempotencyStoreLocal struct {
	keys map[string]IdempotencyRecord
	mu   sync.Mutex
}

func NewIdempotencyStoreLocal() *IdempotencyStoreLocal {
	return &IdempotencyStoreLocal{keys: make(map[string]IdempotencyRecord)}
}

func (m *IdempotencyStoreLocal) Get(key string) (*IdempotencyRecord, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.get(key)
}

func (m *IdempotencyStoreLocal) get(key string) (*IdempotencyRecord, bool, error) {
	v, ok := m.keys[key]
	if !ok {
		return nil, false, nil
	}

	if v.ExpiryDate.After(time.Now()) {
		return &v, true, nil
	}

	delete(m.keys, key)
	return nil, false, nil
}

func (m *IdempotencyStoreLocal) TryReserve(key string, expiry time.Duration) (bool, *IdempotencyRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, exists, err := m.get(key)
	if err != nil {
		return false, nil, err
	}
	if exists {
		return false, existing, nil
	}

	m.keys[key] = IdempotencyRecord{Key: key, ExpiryDate: time.Now().Add(expiry)}
	return true, nil, nil
}

func (m *IdempotencyStoreLocal) SetResponse(
	key string,
	record IdempotencyRecord,
	expiry time.Duration,
) error {
	record.Key = key
	record.ExpiryDate = time.Now().Add(expiry)
	record.Completed = true

	m.mu.Lock()
	defer m.mu.Unlock()

	m.keys[key] = record
	return nil
}

func (m *IdempotencyStoreLocal) Release(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.keys, key)
	return nil
}

type replayResponseRecorder struct {
	statusCode int
	body       bytes.Buffer
}

func (r *replayResponseRecorder) writeHeader(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
	return func(statusCode int) {
		if r.statusCode != 0 {
			return
		}

		r.statusCode = statusCode
		next(statusCode)
	}
}

func (r *replayResponseRecorder) write(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
	return func(body []byte) (int, error) {
		if r.statusCode == 0 {
			r.statusCode = http.StatusOK
		}

		r.body.Write(body)
		return next(body)
	}
}

func captureIdempotencyResponse(rw http.ResponseWriter) (*replayResponseRecorder, http.ResponseWriter) {
	recorder := &replayResponseRecorder{}
	wrapped := httpsnoop.Wrap(rw, httpsnoop.Hooks{
		Write:       recorder.write,
		WriteHeader: recorder.writeHeader,
	})

	return recorder, wrapped
}

func (r *replayResponseRecorder) finalStatusCode() int {
	if r.statusCode != 0 {
		return r.statusCode
	}

	return http.StatusOK
}

func replayIdempotencyResponse(rw http.ResponseWriter, record *IdempotencyRecord) {
	if record.ContentType != "" {
		rw.Header().Set("Content-Type", record.ContentType)
	}
	rw.WriteHeader(record.StatusCode)
	rw.Write(record.Body) // nolint
}

// IdempotencyHandler returns a http.HandlerFunc that checks
// for request idempotency when applicable
func IdempotencyHandler(h http.Handler, opts IdempotencyHandlerOptions, store IdempotencyStore) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// Check for ignored paths
		for _, path := range opts.IgnorePaths {
			if strings.HasPrefix(r.URL.Path, path) {
				h.ServeHTTP(rw, r)
				return
			}
		}

		// Only POST requests are checked
		if r.Method != http.MethodPost {
			h.ServeHTTP(rw, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if len(key) == 0 && r.Method == http.MethodPost {
			http.Error(rw, "Idempotency-Key header not found", http.StatusBadRequest)
			return
		}

		reserved, record, err := store.TryReserve(key, opts.Expiry)
		if err != nil {
			log.
				WithFields(log.Fields{"error": err, "key": key}).
				Warn("Error while reserving idempotency key")
			http.Error(rw, "Error while reserving idempotency key", http.StatusInternalServerError)
			return
		}

		if !reserved {
			if record != nil && record.Completed {
				replayIdempotencyResponse(rw, record)
				return
			}

			http.Error(rw, fmt.Sprintf("Idempotency-Key conflict, key: %s", key), http.StatusConflict)
			return
		}

		recorder, wrapped := captureIdempotencyResponse(rw)
		h.ServeHTTP(wrapped, r)

		finalStatus := recorder.finalStatusCode()
		if finalStatus >= 500 {
			if releaseErr := store.Release(key); releaseErr != nil {
				log.
					WithFields(log.Fields{"error": releaseErr, "key": key, "status": finalStatus}).
					Warn("Error while releasing idempotency reservation after 5xx")
			}
			return
		}

		saveRecord := IdempotencyRecord{
			StatusCode:  finalStatus,
			ContentType: rw.Header().Get("Content-Type"),
			Body:        recorder.body.Bytes(),
		}
		if err := persistIdempotencyResponse(store, key, saveRecord, opts.Expiry); err != nil {
			log.
				WithFields(log.Fields{"error": err, "key": key, "status": finalStatus}).
				Error("Failed to persist idempotency response after retries; client may be unable to replay this response")
		}
	})
}

func persistIdempotencyResponse(store IdempotencyStore, key string, record IdempotencyRecord, expiry time.Duration) error {
	var lastErr error
	for attempt := 0; attempt < idempotencyResponseSaveAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(idempotencyResponseSaveBackoff)
		}
		if err := store.SetResponse(key, record, expiry); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
