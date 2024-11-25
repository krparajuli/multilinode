package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

// LinodeAccount represents a single Linode account with its associated resources
type LinodeAccount struct {
	Name                   string
	AmountSinceLastInvoice float64
	Linodes                []linodego.Instance
	IPs                    map[int][]string
	BillingInfo            *linodego.Invoice
	Error                  string // Store any account-specific errors
}

// PageData contains all data needed to render the dashboard
type PageData struct {
	Accounts      []LinodeAccount
	ErrorMessage  string
	TotalLinodes  int
	TotalBilling  float64
	TotalUnbilled float64
}

// createLinodeClient creates a new authenticated Linode API client
func createLinodeClient(token string) linodego.Client {
	if token == "" {
		log.Fatal("Linode API token cannot be empty")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
		Timeout: 30 * time.Second, // Add timeout to prevent hanging requests
	}

	client := linodego.NewClient(oauth2Client)
	return client
}

// getAccountData retrieves all relevant data for a single Linode account
// Returns a LinodeAccount struct with any errors stored in the Error field
func getAccountData(ctx context.Context, token, accountName string) LinodeAccount {
	account := LinodeAccount{
		Name:                   accountName,
		Linodes:                []linodego.Instance{},
		IPs:                    make(map[int][]string),
		BillingInfo:            nil,
		AmountSinceLastInvoice: 0,
	}

	// Create client with timeout context
	client := createLinodeClient(token)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Get account information
	linodeAccount, err := client.GetAccount(ctx)
	if err != nil {
		errMsg := fmt.Sprintf("Error fetching account info: %v", err)
		log.Printf("[Account: %s] %s", accountName, errMsg)
		account.Error = errMsg
		return account
	}
	account.AmountSinceLastInvoice = float64(linodeAccount.BalanceUninvoiced)

	// Get Linodes with error handling
	linodes, err := client.ListInstances(ctx, nil)
	if err != nil {
		errMsg := fmt.Sprintf("Error fetching Linodes: %v", err)
		log.Printf("[Account: %s] %s", accountName, errMsg)
		account.Error = errMsg
		return account
	}
	account.Linodes = linodes

	// Get IPs for each Linode
	for _, linode := range linodes {
		ips, err := client.GetInstanceIPAddresses(ctx, linode.ID)
		if err != nil {
			log.Printf("[Account: %s] Error fetching IPs for Linode %d: %v", accountName, linode.ID, err)
			continue // Continue with other Linodes even if one fails
		}

		ipList := []string{}

		// Collect IPv4 addresses
		if ips.IPv4 != nil {
			for _, ip := range ips.IPv4.Public {
				ipList = append(ipList, ip.Address)
			}
			for _, ip := range ips.IPv4.Private {
				ipList = append(ipList, ip.Address)
			}
		}

		// Collect IPv6 addresses
		if ips.IPv6 != nil && len(ips.IPv6.Global) > 0 {
			for _, ipv6Range := range ips.IPv6.Global {
				ipList = append(ipList, ipv6Range.Range)
			}
		}

		account.IPs[linode.ID] = ipList
	}

	// Get billing information
	invoices, err := client.ListInvoices(ctx, nil)
	if err != nil {
		log.Printf("[Account: %s] Error fetching invoices: %v", accountName, err)
	} else if len(invoices) > 0 {
		account.BillingInfo = &invoices[0]
	}

	return account
}

// initializeServer sets up and starts the HTTP server
func initializeServer() error {
	// Load environment variables
	if err := godotenv.Load(); err != nil {
		return fmt.Errorf("error loading .env file: %v", err)
	}

	// Set up static file serving
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Set up main handler
	http.HandleFunc("/", handleDashboard())

	return nil
}

// handleDashboard returns the main dashboard handler function
func handleDashboard() http.HandlerFunc {
	// Parse templates at startup
	tmpl, err := template.ParseFiles("templates/index.html")
	if err != nil {
		log.Fatalf("Error parsing template: %v", err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		data := PageData{}
		ctx := r.Context() // Use request context for cancellation

		// Process each account
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "LINODE_TOKEN_") {
				continue
			}

			parts := strings.Split(env, "=")
			if len(parts) != 2 {
				log.Printf("Invalid environment variable format: %s", parts[0])
				continue
			}

			tokenKey := parts[0]
			token := os.Getenv(tokenKey)
			if token == "" {
				log.Printf("Empty token for key: %s", tokenKey)
				continue
			}

			// Get account name from corresponding env var
			accountNum := strings.TrimPrefix(tokenKey, "LINODE_TOKEN_")
			accountName := os.Getenv("LINODE_ACCOUNT_" + accountNum + "_NAME")
			if accountName == "" {
				accountName = "Account " + accountNum
			}

			account := getAccountData(ctx, token, accountName)
			data.Accounts = append(data.Accounts, account)
		}

		// Calculate totals
		for _, account := range data.Accounts {
			data.TotalLinodes += len(account.Linodes)
			if account.BillingInfo != nil {
				data.TotalBilling += float64(account.BillingInfo.Total)
			}
			data.TotalUnbilled += account.AmountSinceLastInvoice
		}

		// Set response headers
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Execute template with error handling
		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Error executing template: %v", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

func main() {
	// Initialize server with error handling
	if err := initializeServer(); err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	serverAddr := ":" + port
	server := &http.Server{
		Addr:         serverAddr,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server starting on http://localhost%s", serverAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
