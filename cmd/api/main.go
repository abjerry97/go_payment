package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/abjerry97/go_payment/internal/processors"
	"github.com/abjerry97/go_payment/internal/server"
	"github.com/abjerry97/go_payment/internal/tools"
)

func main() {
	config := tools.LoadConfig()
	ctx := context.Background()

	db, err := tools.NewDatabaseService(ctx, config.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	redisService, err := tools.NewRedisService(config.RedisURL)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer redisService.Close()

	processor := processors.NewPaymentProcessor(db, redisService, config.WorkerCount)
	processor.Start(ctx)

	server := server.NewAPIServer(db, redisService, processor)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		log.Println("Shutting down...")
		processor.Stop()
		os.Exit(0)
	}()

	log.Printf("Server starting on port %s", config.Port)
	if err := server.Run(":" + config.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
