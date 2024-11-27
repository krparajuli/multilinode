package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"your-module-name/src/home"
	"your-module-name/src/login"

	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
)

var (
	key   []byte
	store *sessions.CookieStore
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	keyString := os.Getenv("ENCRYPTION_KEY")
	if len(keyString) != 32 {
		log.Fatal("ENCRYPTION_KEY must be 32 bytes long")
	}
	key = []byte(keyString)

	store = sessions.NewCookieStore(key)
}

func main() {
	// Create handlers
	loginHandler := login.NewHandler(store, key)
	homeHandler := home.NewHandler(store, key)

	// Set up routes
	http.Handle("/login", loginHandler)
	http.Handle("/", homeHandler)

	// Set up static files
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	serverAddr := ":" + port

	server := &http.Server{
		Addr:         serverAddr,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	log.Printf("Server starting on http://localhost%s", serverAddr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
