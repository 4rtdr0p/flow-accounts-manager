package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
	Get(key string) (*IdempotencyRecord, bool, error)
	SetPending(key string, expiry time.Duration) error
	SetResponse(key string, record IdempotencyRecord, expiry time.Duration) error
}

// Redis store for idempotency keys
type IdempotencyStoreRedis struct {
	conn   redis.Conn
	prefix string
}

func NewIdempotencyStoreRedis(c redis.Conn) *IdempotencyStoreRedis {
	return &IdempotencyStoreRedis{conn: c, prefix: "idempotencykey"}
}

func (r *IdempotencyStoreRedis) prefixedKey(key string) string {
	return fmt.Sprintf("%s:%s", r.prefix, key)
}

func (r *IdempotencyStoreRedis) Get(key string) (*IdempotencyRecord, bool, error) {
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

func (r *IdempotencyStoreRedis) SetPending(key string, expiry time.Duration) error {
	record := IdempotencyRecord{Key: key, ExpiryDate: time.Now().Add(expiry)}
	return r.setRecord(key, record, expiry)
}

func (r *IdempotencyStoreRedis) SetResponse(key string, record IdempotencyRecord, expiry time.Duration) error {
	record.Key = key
	record.ExpiryDate = time.Now().Add(expiry)
	record.Completed = true
	return r.setRecord(key, record, expiry)
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

func (g *IdempotencyStoreGorm) SetPending(key string, expiry time.Duration) error {
	// update expiry date if exists or create a new item
	err := g.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"expiry_date", "completed"}),
	}).Create(&IdempotencyRecord{Key: key, ExpiryDate: time.Now().Add(expiry)}).Error

	if err != nil {
		return err
	}

	return nil
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

// Prune deletes all expired IdempotencyStoreGormItems from the database
func (g *IdempotencyStoreGorm) Prune() error {
	err := g.db.Delete(IdempotencyRecord{}, "expiry_date < ?", time.Now()).Error
	return err
}

// Local / in-memory store for idempotency keys, mainly for testing purposes
type IdempotencyStoreLocal struct {
	keys map[string]IdempotencyRecord
}

func NewIdempotencyStoreLocal() *IdempotencyStoreLocal {
	return &IdempotencyStoreLocal{make(map[string]IdempotencyRecord)}
}

func (m *IdempotencyStoreLocal) Get(key string) (*IdempotencyRecord, bool, error) {
	v, ok := m.keys[key]
	if !ok {
		return nil, false, nil
	}

	// Still valid
	if v.ExpiryDate.After(time.Now()) {
		return &v, true, nil
	}

	// Expired
	// NOTE: item is removed as a side effect
	if v.ExpiryDate.Before(time.Now()) {
		delete(m.keys, key)
		return nil, false, nil
	}

	return nil, false, nil
}

func (m *IdempotencyStoreLocal) SetPending(key string, expiry time.Duration) error {
	m.keys[key] = IdempotencyRecord{Key: key, ExpiryDate: time.Now().Add(expiry)}

	return nil
}

func (m *IdempotencyStoreLocal) SetResponse(
	key string,
	record IdempotencyRecord,
	expiry time.Duration,
) error {
	record.Key = key
	record.ExpiryDate = time.Now().Add(expiry)
	record.Completed = true
	m.keys[key] = record

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

		record, exists, err := store.Get(key)
		if err != nil {
			log.
				WithFields(log.Fields{"error": err, "key": key}).
				Warn("Error while reading idempotency key from storage")
			http.Error(rw, "Error while reading idempotency key", http.StatusInternalServerError)
			return
		}

		if exists {
			if record != nil && record.Completed {
				replayIdempotencyResponse(rw, record)
				return
			}

			http.Error(rw, fmt.Sprintf("Idempotency-Key conflict, key: %s", key), http.StatusConflict)
			return
		} else {
			err := store.SetPending(key, opts.Expiry)
			if err != nil {
				log.
					WithFields(log.Fields{"error": err, "key": key}).
					Warn("Error while saving used idempotency key")
				http.Error(rw, "Error while saving used idempotency key", http.StatusInternalServerError)
				return
			}
		}

		recorder, wrapped := captureIdempotencyResponse(rw)
		h.ServeHTTP(wrapped, r)

		err = store.SetResponse(key, IdempotencyRecord{
			StatusCode:  recorder.finalStatusCode(),
			ContentType: rw.Header().Get("Content-Type"),
			Body:        recorder.body.Bytes(),
		}, opts.Expiry)
		if err != nil {
			log.
				WithFields(log.Fields{"error": err, "key": key}).
				Warn("Error while saving idempotency response")
		}
	})
}
