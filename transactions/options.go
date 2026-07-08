package transactions

import "go.uber.org/ratelimit"

type ServiceOption func(*ServiceImpl)

// CustodialSigningGuard rejects transaction building for non-custodial accounts.
type CustodialSigningGuard func(address string) error

func WithTxRatelimiter(limiter ratelimit.Limiter) ServiceOption {
	return func(svc *ServiceImpl) {
		svc.txRateLimiter = limiter
	}
}

func WithCustodialSigningGuard(guard CustodialSigningGuard) ServiceOption {
	return func(svc *ServiceImpl) {
		svc.custodialSigningGuard = guard
	}
}
