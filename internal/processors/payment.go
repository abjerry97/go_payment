package processors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/abjerry97/go_payment/api"
	"github.com/abjerry97/go_payment/internal/tools"
	"github.com/go-redis/redis/v8"
	log "github.com/sirupsen/logrus"
)

type PaymentProcessor struct {
	db          *tools.DatabaseService
	redis       *tools.RedisService
	WorkerCount int
	wg          sync.WaitGroup
	stopChan    chan struct{}
}

func NewPaymentProcessor(db *tools.DatabaseService, redis *tools.RedisService, WorkerCount int) *PaymentProcessor {
	return &PaymentProcessor{
		db:          db,
		redis:       redis,
		WorkerCount: WorkerCount,
		stopChan:    make(chan struct{}),
	}
}

func (p *PaymentProcessor) Start(ctx context.Context) {
	log.Printf("Starting %d payment processors", p.WorkerCount)

	for i := 0; i < p.WorkerCount; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

func (p *PaymentProcessor) Stop() {
	log.Println("Stopping payment processors...")
	close(p.stopChan)
	p.wg.Wait()
	log.Println("All processors stopped")
}

func (p *PaymentProcessor) worker(ctx context.Context, workerID int) {
	defer p.wg.Done()
	log.Printf("Worker %d started", workerID)

	for {
		select {
		case <-p.stopChan:
			return
		default:
			if err := p.processNextPayment(ctx); err != nil {
				if err != redis.Nil {
					log.Printf("Worker %d error: %v", workerID, err)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	}
}

func (p *PaymentProcessor) processNextPayment(ctx context.Context) error {

	payment, err := p.redis.DequeuePayment(ctx, 1*time.Second)
	if err != nil {
		return err
	}

	if payment == nil {
		return nil
	}

	return p.processPayment(ctx, payment)
}

func (p *PaymentProcessor) processPayment(ctx context.Context, payment *api.PaymentPayload) error {

	processed, err := p.db.IsTransactionProcessed(ctx, payment.TransactionReference)
	if err != nil {
		return err
	}

	if processed {
		log.Printf("Transaction already processed: %s", payment.TransactionReference)
		return nil
	}

	var amount float64
	if _, err := fmt.Sscanf(payment.TransactionAmount, "%f", &amount); err != nil {
		return fmt.Errorf("invalid amount: %v", err)
	}

	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {

		customer, err := p.db.GetCustomer(ctx, payment.CustomerID)
		if err != nil {
			return fmt.Errorf("failed to get customer: %v", err)
		}

		success, err := p.db.UpdateCustomerBalance(
			ctx,
			payment.CustomerID,
			amount,
			payment.TransactionDate,
			customer.Version,
		)

		if err != nil {
			return fmt.Errorf("failed to update balance: %v", err)
		}

		if success {

			if err := p.db.MarkTransactionProcessed(ctx, payment.TransactionReference, payment.CustomerID, amount); err != nil {
				log.Printf("Warning: failed to mark transaction as processed: %v", err)
			}

			if err := p.redis.MarkDuplicate(ctx, payment.TransactionReference, 24*time.Hour); err != nil {
				log.Printf("Warning: failed to cache duplicate: %v", err)
			}

			newBalance := customer.OutstandingBalance - amount
			if newBalance < 0 {
				newBalance = 0
			}

			if err := p.redis.CacheBalance(ctx, payment.CustomerID, newBalance, 5*time.Minute); err != nil {
				log.Printf("Warning: failed to cache balance: %v", err)
			}

			log.Printf("Processed payment: %s - Amount: %.2f - Balance: %.2f",
				payment.CustomerID, amount, newBalance)
			return nil
		}

		log.Printf("Version conflict for %s, retry %d", payment.CustomerID, attempt+1)
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}

	return fmt.Errorf("failed after %d retries", maxRetries)
}
