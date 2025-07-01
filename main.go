// main.go
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// Nema više potrebe za importovanjem "encoding/json", "fmt", "strconv" ovde
	// jer su ti importi sada u api.go
	// Nema više potrebe za importovanjem "github.com/gorilla/mux" ovde, jer se
	// koristi unutar APIServera
)

func main() {
	// Učitavanje konfiguracije
	config, err := LoadConfigFromFile("config.json")
	if err != nil {
		log.Fatalf("Fatal: Greška pri učitavanju konfiguracije: %v", err)
	}

	// Inicijalizacija AppConfig
	appConfig, err := NewAppConfig(config)
	if err != nil {
		log.Fatalf("Fatal: Greška pri inicijalizaciji AppConfig: %v", err)
	}

	// Inicijalizacija baze podataka
	dataset, err := NewSQLDataset(appConfig)
	if err != nil {
		log.Fatalf("Fatal: Greška pri inicijalizaciji baze podataka: %v", err)
	}
	defer dataset.Close() // Zatvara vezu sa bazom podataka kada se main završi

	// Inicijalizacija API servera
	apiServer := NewAPIServer(appConfig, dataset) // Kreiramo instancu APIServera

	// Postavljanje HTTP servera
	serverAddr := ":8080" // Može se prebaciti u config
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: apiServer.router, // Sada koristimo router iz APIServer instance
		// Dobra praksa je postaviti timeout-e
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Pokretanje servera u gorutini
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Logovanje greške ako server ne uspe da se pokrene (npr. port je zauzet)
			log.Fatalf("Fatal: Greška pri pokretanju servera: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("INFO: Gašenje servera...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Fatal: Server se nije ugasio gracefuly: %v", err)
	}

	log.Println("INFO: Server je ugašen.")
}

// VAŽNO: Sve handler funkcije (GetAllModules, GetModuleRecords, itd.)
// su sada premeštene u api.go fajl kao metode APIServer strukture.
// Zato ih nema više u ovom fajlu.
