// Package mongo provides a read-only Mongo client for reading Payload-Galaxy
// pricing data (pricing-configurations and studio-quotes collections).
//
// It mirrors the initialization pattern used for the SQL connection in
// datastore/gorm: a New(cfg) constructor that returns a connected client and a
// Close(client) helper for graceful shutdown.
package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/flow-hydraulics/flow-wallet-api/configs"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Client wraps the underlying mongo.Client along with the database and
// collection names used to read pricing data.
type Client struct {
	client *mongo.Client
	db     *mongo.Database
}

// New connects to the Mongo instance described by cfg and returns a read-only
// client. When cfg.MongoURI is empty, no connection is attempted and a nil
// client is returned (Mongo is optional for deployments that don't use Studio
// pricing).
func New(cfg *configs.Config) (*Client, error) {
	if cfg.MongoURI == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.MongoConnectTimeout)
	defer cancel()

	opts := options.Client().
		ApplyURI(cfg.MongoURI).
		SetConnectTimeout(cfg.MongoConnectTimeout).
		SetServerSelectionTimeout(cfg.MongoConnectTimeout)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("connect to mongo: %w", err)
	}

	// Verify the connection is actually usable before returning.
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("ping mongo: %w", err)
	}

	return &Client{
		client: client,
		db:     client.Database(cfg.MongoDatabase),
	}, nil
}

// Close gracefully disconnects the underlying Mongo client. It is safe to call
// on a nil receiver.
func (c *Client) Close() {
	if c == nil || c.client == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.client.Disconnect(ctx); err != nil {
		// Disconnect errors are non-fatal during shutdown; the process is
		// exiting anyway.
		_ = err
	}
}

// Collection returns a handle to the named collection within the configured
// database. It is a read-only handle; callers must not perform writes.
func (c *Client) Collection(name string) *mongo.Collection {
	return c.db.Collection(name)
}

// Database returns the configured database handle.
func (c *Client) Database() *mongo.Database {
	return c.db
}
