package transactions

import (
	"context"

	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/onflow/flow-go-sdk"
	log "github.com/sirupsen/logrus"
)

const TransactionJobType = "transaction"

// ResultExtractorFunc lets a plugin derive a job-visible Result payload
// (typically a small JSON blob) from the events emitted by one of its own
// transaction Types. The core transactions package never needs to know
// anything about a plugin's event shapes: plugins register their own
// extractor per Type via ServiceImpl.RegisterResultExtractor, and
// executeTransactionJob looks it up generically after the transaction
// completes. A failing or unregistered extractor never fails the job — the
// on-chain transaction already succeeded — it only leaves Result empty,
// exactly today's behavior.
type ResultExtractorFunc func(events []flow.Event) (string, error)

func (s *ServiceImpl) executeTransactionJob(ctx context.Context, j *jobs.Job) error {
	if j.Type != TransactionJobType {
		return jobs.ErrInvalidJobType
	}

	j.ShouldSendNotification = true

	tx, err := s.store.Transaction(j.TransactionID)
	if err != nil {
		return err
	}

	err = s.sendTransaction(ctx, &tx)
	if err != nil {
		return err
	}

	if extractor, ok := s.resultExtractors[tx.TransactionType]; ok {
		result, err := extractor(tx.Events)
		if err != nil {
			log.WithError(err).WithFields(log.Fields{
				"jobID":  j.ID,
				"txType": tx.TransactionType,
			}).Warn("transactions: result extractor failed; job completes without Result")
		} else {
			j.Result = result
		}
	}

	return nil
}
