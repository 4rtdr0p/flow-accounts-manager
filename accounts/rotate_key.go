package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/flow-hydraulics/flow-wallet-api/errors"
	"github.com/flow-hydraulics/flow-wallet-api/flow_helpers"
	"github.com/flow-hydraulics/flow-wallet-api/jobs"
	"github.com/flow-hydraulics/flow-wallet-api/keys"
	"github.com/flow-hydraulics/flow-wallet-api/templates/template_strings"
	"github.com/flow-hydraulics/flow-wallet-api/transactions"
	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
	log "github.com/sirupsen/logrus"
)

const RotateKeyJobType = "rotate_key"

type rotateKeyJobAttributes struct {
	Address string `json:"address"`
}

// RotateKeyResult is returned after a successful key rotation.
type RotateKeyResult struct {
	NewKeyID      int    `json:"newKeyId"`
	NewKeyIndex   int    `json:"newKeyIndex"`
	OldKeyIndex   int    `json:"oldKeyIndex"`
	OldKeyRevoked bool   `json:"oldKeyRevoked"`
	TransactionID string `json:"transactionId,omitempty"`
}

// RotateKey replaces the active custodial signing key atomically on-chain.
func (s *ServiceImpl) RotateKey(ctx context.Context, sync bool, address string) (*jobs.Job, *RotateKeyResult, error) {
	address, err := flow_helpers.ValidateAddress(address, s.cfg.ChainID)
	if err != nil {
		return nil, nil, err
	}

	if !sync {
		attrs := rotateKeyJobAttributes{Address: address}
		attrBytes, err := json.Marshal(attrs)
		if err != nil {
			return nil, nil, err
		}

		job, err := s.wp.CreateJob(RotateKeyJobType, "", jobs.WithAttributes(attrBytes))
		if err != nil {
			return nil, nil, err
		}
		if err := s.wp.Schedule(job); err != nil {
			return nil, nil, err
		}

		return job, nil, nil
	}

	result, err := s.rotateKey(ctx, address)
	return nil, result, err
}

func (s *ServiceImpl) rotateKey(ctx context.Context, address string) (*RotateKeyResult, error) {
	entry := log.WithFields(log.Fields{"address": address, "function": "ServiceImpl.rotateKey"})

	dbAccount, err := s.store.Account(address)
	if err != nil {
		return nil, err
	}

	if dbAccount.Type != AccountTypeCustodial {
		return nil, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("key rotation is only supported for custodial accounts"),
		}
	}

	if len(dbAccount.Keys) == 0 {
		return nil, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("account has no managed keys"),
		}
	}

	flowAddress := flow.HexToAddress(address)
	authorizer, err := s.km.UserAuthorizer(ctx, flowAddress)
	if err != nil {
		return nil, err
	}

	oldKeyIndex := int(authorizer.Key.Index)
	if authorizer.Key.Revoked {
		return nil, &errors.RequestError{
			StatusCode: http.StatusBadRequest,
			Err:        fmt.Errorf("signing key at index %d is already revoked", oldKeyIndex),
		}
	}

	var signingKey *keys.Storable
	for i := range dbAccount.Keys {
		if dbAccount.Keys[i].Index == oldKeyIndex {
			signingKey = &dbAccount.Keys[i]
			break
		}
	}
	if signingKey == nil {
		return nil, fmt.Errorf("managed signing key at index %d not found in database", oldKeyIndex)
	}

	flowAccount, err := s.fc.GetAccount(ctx, flowAddress)
	if err != nil {
		entry.WithFields(log.Fields{"err": err}).Error("failed to get Flow account")
		return nil, err
	}

	newKeyIndex := len(flowAccount.Keys)
	weight := s.cfg.DefaultKeyWeight
	if weight < 0 {
		weight = flow.AccountKeyWeightThreshold
	}

	accountKey, newPrivateKey, err := s.km.Generate(ctx, newKeyIndex, weight)
	if err != nil {
		return nil, err
	}

	publicKeyHex := strings.TrimPrefix(accountKey.PublicKey.String(), "0x")
	publicKeyArg, err := cadence.NewString(publicKeyHex)
	if err != nil {
		return nil, err
	}
	args := []transactions.Argument{
		publicKeyArg,
		cadence.NewInt(oldKeyIndex),
	}

	_, tx, err := s.txs.Create(
		ctx,
		true,
		address,
		template_strings.RotateKeyTransaction,
		args,
		transactions.KeyRotate,
	)
	if err != nil {
		entry.WithFields(log.Fields{"err": err}).Error("failed to rotate account key on-chain")
		return nil, err
	}

	flowAccountAfter, err := s.fc.GetAccount(ctx, flowAddress)
	if err != nil {
		return nil, err
	}

	if oldKeyIndex >= len(flowAccountAfter.Keys) || !flowAccountAfter.Keys[oldKeyIndex].Revoked {
		return nil, fmt.Errorf("old key at index %d was not revoked after rotation transaction", oldKeyIndex)
	}

	if newKeyIndex >= len(flowAccountAfter.Keys) {
		return nil, fmt.Errorf("new key at index %d not found after rotation transaction", newKeyIndex)
	}
	if flowAccountAfter.Keys[newKeyIndex].Revoked {
		return nil, fmt.Errorf("new key at index %d is revoked after rotation transaction", newKeyIndex)
	}

	encryptedKey, err := s.km.Save(*newPrivateKey)
	if err != nil {
		return nil, err
	}
	encryptedKey.AccountAddress = address
	encryptedKey.PublicKey = accountKey.PublicKey.String()
	encryptedKey.Index = newKeyIndex

	if err := s.store.RotateKeyState(signingKey.ID, &encryptedKey); err != nil {
		entry.WithFields(log.Fields{"err": err, "oldKeyId": signingKey.ID}).Error("failed to persist rotated key state")
		return nil, err
	}

	return &RotateKeyResult{
		NewKeyID:      encryptedKey.ID,
		NewKeyIndex:   newKeyIndex,
		OldKeyIndex:   oldKeyIndex,
		OldKeyRevoked: true,
		TransactionID: tx.TransactionId,
	}, nil
}

func (s *ServiceImpl) executeRotateKeyJob(ctx context.Context, j *jobs.Job) error {
	if j.Type != RotateKeyJobType {
		return jobs.ErrInvalidJobType
	}

	j.ShouldSendNotification = true

	var attrs rotateKeyJobAttributes
	if err := json.Unmarshal(j.Attributes, &attrs); err != nil {
		return err
	}

	result, err := s.rotateKey(ctx, attrs.Address)
	if err != nil {
		return err
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return err
	}

	j.TransactionID = result.TransactionID
	j.Result = string(resultBytes)

	return nil
}
