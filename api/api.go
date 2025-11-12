package api

import "time"

type PaymentStatus string

const (
	StatusComplete PaymentStatus = "COMPLETE"
	StatusPending  PaymentStatus = "PENDING"
	StatusFailed   PaymentStatus = "FAILED"
)

type PaymentPayload struct {
	CustomerID           string        `json:"customer_id" binding:"required,startswith=GIG"`
	PaymentStatus        PaymentStatus `json:"payment_status" binding:"required"`
	TransactionAmount    string        `json:"transaction_amount" binding:"required"`
	TransactionDate      string        `json:"transaction_date" binding:"required"`
	TransactionReference string        `json:"transaction_reference" binding:"required"`
}

type PaymentResponse struct {
	Status               string   `json:"status"`
	Message              string   `json:"message"`
	TransactionReference string   `json:"transaction_reference"`
	CustomerID           string   `json:"customer_id"`
	RemainingBalance     *float64 `json:"remaining_balance,omitempty"`
}

type CustomerAccount struct {
	CustomerID         string     `json:"customer_id"`
	AssetValue         float64    `json:"asset_value"`
	TermWeeks          int        `json:"term_weeks"`
	TotalPaid          float64    `json:"total_paid"`
	OutstandingBalance float64    `json:"outstanding_balance"`
	DeploymentDate     time.Time  `json:"deployment_date"`
	LastPaymentDate    *time.Time `json:"last_payment_date,omitempty"`
	PaymentCount       int        `json:"payment_count"`
	Version            int        `json:"version"`
}
