package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jclement/picotunnel/internal/client"
)

var (
	serverAddr = flag.String("server", getEnvOrDefault("PICOTUNNEL_SERVER", ""), "Server address (host:port)")
	token      = flag.String("token", getEnvOrDefault("PICOTUNNEL_TOKEN", ""), "Authentication token")
	insecure   = flag.Bool("insecure", getEnvOrDefault("PICOTUNNEL_INSECURE", "false") == "true", "Skip TLS verification (for development)")
	version    = flag.Bool("version", false, "Show version")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Println("picotunnel-client v1.0.0")
		os.Exit(0)
	}

	if *serverAddr == "" {
		log.Fatal("Server address is required (use --server or PICOTUNNEL_SERVER)")
	}

	if *token == "" {
		log.Fatal("Token is required (use --token or PICOTUNNEL_TOKEN)")
	}

	// Create client
	config := client.Config{
		ServerAddr: *serverAddr,
		Token:      *token,
		Insecure:   *insecure,
	}

	c := client.NewClient(config)

	// Handle shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start client
	if err := c.Start(); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	log.Printf("Client started successfully. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-sigChan
	log.Printf("Shutdown signal received")

	// Stop client
	if err := c.Stop(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Printf("Client stopped")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}