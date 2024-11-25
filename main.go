package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

type LinodeAccount struct {
	Name                   string
	AmountSinceLastInvoice float64
	Linodes                []linodego.Instance
	IPs                    map[int][]string
	BillingInfo            *linodego.Invoice
}

type PageData struct {
	Accounts      []LinodeAccount
	ErrorMessage  string
	TotalLinodes  int
	TotalBilling  float64
	TotalUnbilled float64
}

type BillingInfo struct {
	Date  string
	Total float64
}

func createLinodeClient(token string) linodego.Client {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token,
	})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	return linodego.NewClient(oauth2Client)
}

func getAccountData(ctx context.Context, token, accountName string) LinodeAccount {
	account := LinodeAccount{
		Name:                   accountName,
		Linodes:                []linodego.Instance{},
		IPs:                    make(map[int][]string),
		BillingInfo:            nil,
		AmountSinceLastInvoice: 0,
	}

	client := createLinodeClient(token)

	linodeAccount, err := client.GetAccount(ctx)
	if err != nil {
		log.Printf("Error fetching account info for account %s: %v", accountName, err)
		return account
	}
	account.AmountSinceLastInvoice = float64(linodeAccount.BalanceUninvoiced)

	// Get Linodes
	linodes, err := client.ListInstances(ctx, nil)
	if err != nil {
		log.Printf("Error fetching Linodes for account %s: %v", accountName, err)
		return account
	}
	account.Linodes = linodes

	// Get IPs for each Linode
	for _, linode := range linodes {
		ips, err := client.GetInstanceIPAddresses(ctx, linode.ID)
		if err != nil {
			fmt.Println("Error fetching IP addresses:", err)
			continue
		}

		ipList := []string{}

		if ips.IPv4 != nil {
			for _, ip := range ips.IPv4.Public {
				ipList = append(ipList, ip.Address)
			}
			for _, ip := range ips.IPv4.Private {
				ipList = append(ipList, ip.Address)
			}
		}

		// Add IPv6 addresses
		if ips.IPv6 != nil && len(ips.IPv6.Global) > 0 {
			for _, ipv6Range := range ips.IPv6.Global {
				ipList = append(ipList, ipv6Range.Range)
			}
		}

		account.IPs[linode.ID] = ipList
	}

	// Get billing info
	invoices, err := client.ListInvoices(ctx, nil)
	if err == nil && len(invoices) > 0 {
		account.BillingInfo = &invoices[0]
	}

	return account
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{}
		ctx := context.Background()

		// Get all environment variables
		envVars := os.Environ()

		// Process each account
		for _, env := range envVars {
			if strings.HasPrefix(env, "LINODE_TOKEN_") {
				parts := strings.Split(env, "=")
				if len(parts) != 2 {
					continue
				}

				tokenKey := parts[0]
				token := os.Getenv(tokenKey)

				// Get account name from corresponding env var
				accountNum := strings.TrimPrefix(tokenKey, "LINODE_TOKEN_")
				accountName := os.Getenv("LINODE_ACCOUNT_" + accountNum + "_NAME")
				if accountName == "" {
					accountName = "Account " + accountNum
				}

				account := getAccountData(ctx, token, accountName)
				data.Accounts = append(data.Accounts, account)
			}
		}

		// Calculate totals after processing all accounts
		for _, account := range data.Accounts {
			data.TotalLinodes += len(account.Linodes)
			if account.BillingInfo != nil {
				data.TotalBilling += float64(account.BillingInfo.Total)
			}
			data.TotalUnbilled += account.AmountSinceLastInvoice
		}

		tmpl, err := template.ParseFiles("templates/index.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	log.Println("Server starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
