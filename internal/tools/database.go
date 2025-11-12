package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/abjerry97/go_payment/api"
	"github.com/jackc/pgx/v5/pgxpool"
	log "github.com/sirupsen/logrus"
)

type DatabaseService struct {
	Pool *pgxpool.Pool
}

func NewDatabaseService(ctx context.Context, databaseURL string) (*DatabaseService, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}

	config.MaxConns = 50
	config.MinConns = 10
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}

	log.Println("Database connected successfully")
	return &DatabaseService{Pool: pool}, nil
}

func (db *DatabaseService) Close() {
	db.Pool.Close()
}

func (db *DatabaseService) GetCustomer(ctx context.Context, customerID string) (*api.CustomerAccount, error) {
	query := `
		SELECT customer_id, asset_value, term_weeks, total_paid, outstanding_balance, 
		       deployment_date, last_payment_date, payment_count, version
		FROM customer_accounts
		WHERE customer_id = $1
	`

	var customer api.CustomerAccount
	err := db.Pool.QueryRow(ctx, query, customerID).Scan(
		&customer.CustomerID,
		&customer.AssetValue,
		&customer.TermWeeks,
		&customer.TotalPaid,
		&customer.OutstandingBalance,
		&customer.DeploymentDate,
		&customer.LastPaymentDate,
		&customer.PaymentCount,
		&customer.Version,
	)

	if err != nil {
		return nil, err
	}

	return &customer, nil
}

func (db *DatabaseService) UpdateCustomerBalance(ctx context.Context, customerID string, amount float64, txnDate string, version int) (bool, error) {
	query := `
		UPDATE customer_accounts
		SET total_paid = total_paid + $2,
		    outstanding_balance = GREATEST(0, asset_value - (total_paid + $2)),
		    last_payment_date = $3,
		    payment_count = payment_count + 1,
		    version = version + 1,
		    updated_at = NOW()
		WHERE customer_id = $1 AND version = $4
		RETURNING outstanding_balance
	`

	var balance float64
	err := db.Pool.QueryRow(ctx, query, customerID, amount, txnDate, version).Scan(&balance)

	if err != nil {
		if err.Error() == "no rows in result set" {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (db *DatabaseService) IsTransactionProcessed(ctx context.Context, txnRef string) (bool, error) {
	query := `SELECT EXISTS(SELECT 1 FROM processed_transactions WHERE transaction_reference = $1)`

	var exists bool
	err := db.Pool.QueryRow(ctx, query, txnRef).Scan(&exists)
	return exists, err
}

func (db *DatabaseService) MarkTransactionProcessed(ctx context.Context, txnRef, customerID string, amount float64) error {
	query := `
		INSERT INTO processed_transactions (transaction_reference, customer_id, amount, processed_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (transaction_reference) DO NOTHING
	`

	_, err := db.Pool.Exec(ctx, query, txnRef, customerID, amount)
	return err
}

func (db *DatabaseService) SeedCustomers(ctx context.Context, count int) error {
	log.Printf("Seeding %d customers...", count)

	query := `
		INSERT INTO customer_accounts (
			customer_id, 
			asset_value, 
			term_weeks, 
			deployment_date
		)
		SELECT 
			'GIG' || LPAD(generate_series::TEXT, 5, '0'),
			1000000.00,
			50,
			NOW() - (random() * INTERVAL '180 days')
		FROM generate_series(1, $1)
		ON CONFLICT (customer_id) DO NOTHING
	`

	result, err := db.Pool.Exec(ctx, query, count)
	if err != nil {
		return fmt.Errorf("failed to seed customers: %v", err)
	}

	rowsAffected := result.RowsAffected()
	log.Printf("Successfully seeded %d customers", rowsAffected)
	return nil
}

func (db *DatabaseService) GetCustomerCount(ctx context.Context) (int, error) {
	var count int
	err := db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM customer_accounts").Scan(&count)
	return count, err
}
