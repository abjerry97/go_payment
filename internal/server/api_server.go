package server

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/abjerry97/go_payment/api"
	"github.com/abjerry97/go_payment/internal/processors"
	"github.com/abjerry97/go_payment/internal/tools"
	"github.com/gin-gonic/gin"
)

type APIServer struct {
	db        *tools.DatabaseService
	redis     *tools.RedisService
	Processor *processors.PaymentProcessor
	router    *gin.Engine
}

func NewAPIServer(db *tools.DatabaseService, redis *tools.RedisService, processor *processors.PaymentProcessor) *APIServer {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("[%s] %s %s %d %s\n",
			param.TimeStamp.Format("2006-01-02 15:04:05"),
			param.Method,
			param.Path,
			param.StatusCode,
			param.Latency,
		)
	}))

	server := &APIServer{
		db:        db,
		redis:     redis,
		Processor: processor,
		router:    router,
	}

	server.setupRoutes()
	return server
}

func (s *APIServer) setupRoutes() {
	s.router.GET("/", s.handleRoot)
	s.router.GET("/api/v1/health", s.handleHealth)
	s.router.POST("/api/v1/payments", s.handlePayment)
	s.router.GET("/api/v1/customers/:customer_id/balance", s.handleGetBalance)
	s.router.GET("/api/v1/customers", s.handleListCustomers)
	s.router.POST("/api/v1/admin/seed-customers", s.handleSeedCustomers)
	s.router.GET("/api/v1/admin/stats", s.handleStats)
}

func (s *APIServer) handleRoot(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service": "Asset Payment Processing API",
		"version": "1.0.0",
		"docs":    "/api/v1/health",
	})
}

func (s *APIServer) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

func (s *APIServer) handlePayment(c *gin.Context) {
	var payment api.PaymentPayload
	if err := c.ShouldBindJSON(&payment); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payment.PaymentStatus != api.StatusComplete {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Only COMPLETE payments accepted. Received: %s", payment.PaymentStatus),
		})
		return
	}

	ctx := c.Request.Context()

	isDup, err := s.redis.IsDuplicate(ctx, payment.TransactionReference)
	if err != nil {
		log.Printf("Redis duplicate check failed: %v", err)

	}

	if isDup {
		customer, _ := s.db.GetCustomer(ctx, payment.CustomerID)
		response := api.PaymentResponse{
			Status:               "duplicate",
			Message:              "Transaction already processed",
			TransactionReference: payment.TransactionReference,
			CustomerID:           payment.CustomerID,
		}
		if customer != nil {
			response.RemainingBalance = &customer.OutstandingBalance
		}
		c.JSON(http.StatusOK, response)
		return
	}

	customer, err := s.db.GetCustomer(ctx, payment.CustomerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	if err := s.redis.EnqueuePayment(ctx, &payment); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to queue payment"})
		return
	}

	cachedBalance, _ := s.redis.GetCachedBalance(ctx, payment.CustomerID)
	currentBalance := customer.OutstandingBalance
	if cachedBalance != nil {
		currentBalance = *cachedBalance
	}

	c.JSON(http.StatusOK, api.PaymentResponse{
		Status:               "accepted",
		Message:              "Payment accepted for processing",
		TransactionReference: payment.TransactionReference,
		CustomerID:           payment.CustomerID,
		RemainingBalance:     &currentBalance,
	})
}

func (s *APIServer) handleGetBalance(c *gin.Context) {
	customerID := c.Param("customer_id")
	ctx := c.Request.Context()

	customer, err := s.db.GetCustomer(ctx, customerID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Customer not found"})
		return
	}

	completionPct := (customer.TotalPaid / customer.AssetValue) * 100

	c.JSON(http.StatusOK, gin.H{
		"customer_id":           customer.CustomerID,
		"asset_value":           customer.AssetValue,
		"total_paid":            customer.TotalPaid,
		"outstanding_balance":   customer.OutstandingBalance,
		"payment_count":         customer.PaymentCount,
		"completion_percentage": fmt.Sprintf("%.2f", completionPct),
		"last_payment_date":     customer.LastPaymentDate,
	})
}

func (s *APIServer) Run(addr string) error {
	return s.router.Run(addr)
}

func (s *APIServer) handleListCustomers(c *gin.Context) {
	ctx := c.Request.Context()

	limit := 20
	offset := 0

	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := c.Query("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}

	if limit > 100 {
		limit = 100
	}

	query := `
		SELECT customer_id, asset_value, term_weeks, total_paid, 
		       outstanding_balance, deployment_date, last_payment_date, 
		       payment_count, version
		FROM customer_accounts
		ORDER BY customer_id
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.Pool.Query(ctx, query, limit, offset)
	fmt.Printf("------------------------------------------")
	fmt.Print(err)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch customers"})
		return
	}
	defer rows.Close()

	customers := []gin.H{}
	for rows.Next() {
		var customer api.CustomerAccount
		err := rows.Scan(
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
			continue
		}

		completionPct := (customer.TotalPaid / customer.AssetValue) * 100
		customers = append(customers, gin.H{
			"customer_id":           customer.CustomerID,
			"asset_value":           customer.AssetValue,
			"total_paid":            customer.TotalPaid,
			"outstanding_balance":   customer.OutstandingBalance,
			"payment_count":         customer.PaymentCount,
			"completion_percentage": fmt.Sprintf("%.2f", completionPct),
		})
	}

	var total int
	s.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM customer_accounts").Scan(&total)

	c.JSON(http.StatusOK, gin.H{
		"customers": customers,
		"total":     total,
		"limit":     limit,
		"offset":    offset,
	})
}

func (s *APIServer) handleSeedCustomers(c *gin.Context) {
	ctx := c.Request.Context()

	var request struct {
		Count int `json:"count" binding:"required,min=1,max=10000"`
	}

	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.db.SeedCustomers(ctx, request.Count); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	count, _ := s.db.GetCustomerCount(ctx)

	c.JSON(http.StatusOK, gin.H{
		"message":         "Customers seeded successfully",
		"requested":       request.Count,
		"total_customers": count,
	})
}

func (s *APIServer) handleStats(c *gin.Context) {
	ctx := c.Request.Context()

	query := `
		SELECT 
			COUNT(*) as total_customers,
			COUNT(*) FILTER (WHERE total_paid > 0) as active_customers,
			COUNT(*) FILTER (WHERE outstanding_balance = 0) as completed_customers,
			COALESCE(SUM(asset_value), 0) as total_deployed_value,
			COALESCE(SUM(total_paid), 0) as total_paid_amount,
			COALESCE(SUM(outstanding_balance), 0) as total_outstanding,
			COALESCE(AVG(total_paid / NULLIF(asset_value, 0) * 100), 0) as avg_completion_rate
		FROM customer_accounts
	`

	var stats struct {
		TotalCustomers     int     `json:"total_customers"`
		ActiveCustomers    int     `json:"active_customers"`
		CompletedCustomers int     `json:"completed_customers"`
		TotalDeployedValue float64 `json:"total_deployed_value"`
		TotalPaidAmount    float64 `json:"total_paid_amount"`
		TotalOutstanding   float64 `json:"total_outstanding"`
		AvgCompletionRate  float64 `json:"avg_completion_rate"`
	}

	err := s.db.Pool.QueryRow(ctx, query).Scan(
		&stats.TotalCustomers,
		&stats.ActiveCustomers,
		&stats.CompletedCustomers,
		&stats.TotalDeployedValue,
		&stats.TotalPaidAmount,
		&stats.TotalOutstanding,
		&stats.AvgCompletionRate,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch statistics"})
		return
	}

	queueSize, _ := s.redis.Client.LLen(ctx, "payment_queue").Result()

	c.JSON(http.StatusOK, gin.H{
		"database": stats,
		"queue": gin.H{
			"size": queueSize,
		},
		"workers": gin.H{
			"count": s.Processor.WorkerCount,
		},
	})
}
