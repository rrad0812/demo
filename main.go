// main.go
package main

import (
	"context"
	"encoding/json" // Dodaj ovo
	"fmt"           // Dodaj ovo (ako već nije)
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv" // Dodaj ovo za strconv.Atoi
	"syscall"
	"time"

	"github.com/gorilla/mux"
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

	// Inicijalizacija routera
	r := mux.NewRouter()

	// Handler za rutu /api/modules
	r.HandleFunc("/api/modules", func(w http.ResponseWriter, req *http.Request) {
		GetAllModules(w, req, appConfig)
	}).Methods("GET")

	// Handler za rute modula
	r.HandleFunc("/api/modules/{moduleID}", func(w http.ResponseWriter, req *http.Request) {
		GetModuleRecords(w, req, appConfig, dataset)
	}).Methods("GET")

	r.HandleFunc("/api/modules/{moduleID}/{recordID}", func(w http.ResponseWriter, req *http.Request) {
		GetSingleRecord(w, req, appConfig, dataset)
	}).Methods("GET")

	r.HandleFunc("/api/modules/{moduleID}", func(w http.ResponseWriter, req *http.Request) {
		CreateRecord(w, req, appConfig, dataset)
	}).Methods("POST")

	r.HandleFunc("/api/modules/{moduleID}/{recordID}", func(w http.ResponseWriter, req *http.Request) {
		UpdateRecord(w, req, appConfig, dataset)
	}).Methods("PUT")

	r.HandleFunc("/api/modules/{moduleID}/{recordID}", func(w http.ResponseWriter, req *http.Request) {
		DeleteRecord(w, req, appConfig, dataset)
	}).Methods("DELETE")

	// Postavljanje HTTP servera
	serverAddr := ":8080" // Može se prebaciti u config
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: r,
		// Dobra praksa je postaviti timeout-e
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Pokretanje servera u gorutini
	go func() {
		log.Printf("INFO: Server pokrenut na http://localhost%s", serverAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Fatal: Greška pri pokretanju servera: %v", err)
		}
	}()

	// Graceful shutdown
	// Kreirajte kanal za osluškivanje signala operativnog sistema
	quit := make(chan os.Signal, 1)
	// Prijavite se na sistemske signale za prekid (Ctrl+C) i terminaciju
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	// Blokirajte dok se ne primi signal
	<-quit
	log.Println("INFO: Gašenje servera...")

	// Kreirajte kontekst sa timeoutom za gašenje
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel() // Otpustite resurse konteksta

	// Pokušajte da se gracefully ugasite
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Fatal: Server se nije ugasio gracefuly: %v", err)
	}

	log.Println("INFO: Server je ugašen.")
}

// GetAllModules handles requests to get all module definitions.
// NOTE: Ovo je pojednostavljena verzija za UI tree, kao što smo diskutovali ranije.
// Kasnije ćeš ovde implementirati logiku za vraćanje tree-strukture za UI.
// func GetAllModules(w http.ResponseWriter, req *http.Request, appConfig *AppConfig) {
//Za sada samo vraćamo sve module direktno, kasnije se može prilagoditi za tree strukturu
// w.Header().Set("Content-Type", "application/json")
// if err := json.NewEncoder(w).Encode(appConfig.Modules); err != nil {
// http.Error(w, fmt.Sprintf("Greška pri enkodiranju modula: %v", err), http.StatusInternalServerError)
// return
// }
// log.Println("INFO: Vraćeni svi moduli.")
// }

// GetAllModules handles requests to get all module definitions in a hierarchical
//(tree) structure for UI.

func GetAllModules(w http.ResponseWriter, req *http.Request, appConfig *AppConfig) {
	// Struktura za čuvanje tree-a za UI
	type UINode struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Type     string   `json:"type"`
		Children []UINode `json:"children,omitempty"`
		Icon     string   `json:"icon,omitempty"` // Opciono, ako želiš ikone
		// Možeš dodati i druge relevantne podatke, npr. route
	}

	var appRoot *UINode = nil
	groupNodes := make(map[string]UINode)  // Mapa za brzi pristup grupama po ID-u
	moduleNodes := make(map[string]UINode) // Mapa za brzi pristup modulima po ID-u
	//(koji nisu grupe)

	// Prvo prođi kroz sve module da popuniš mape i pronađeš "app" root
	for _, moduleDef := range appConfig.Modules {
		node := UINode{
			ID:   moduleDef.ID,
			Name: moduleDef.Name,
			Type: moduleDef.Type,
			// Icon: moduleDef.Icon, // Ako imaš polje 'Icon' u ModuleDefinition
		}

		if moduleDef.Type == "root" { // Pretpostavljamo da tvoj "app" modul
			// ima type: "root"
			appRoot = &node
		} else if moduleDef.Type == "group" {
			groupNodes[moduleDef.ID] = node
		} else { // Standardni moduli (tabele)
			moduleNodes[moduleDef.ID] = node
		}
	}

	// Ako 'app' root nije pronađen, ili ako želiš da app bude virtuelni koren,
	// kreiraćemo ga ovde. Tvoj app.json ima type: "root", pa bi trebalo da bude pronađen.
	if appRoot == nil {
		appRoot = &UINode{
			ID:   "app_root",
			Name: "Aplikacija", // Default name if 'app' module is not type "root"
			Type: "root",
		}
	}

	// Popuni decu za "app" koren (njegove grupe)
	// Ovde koristimo polje "groups" iz app.json
	if appDef := appConfig.GetModuleByID("app"); appDef != nil && appDef.Groups != nil {
		for _, groupLink := range appDef.Groups {
			if groupNode, ok := groupNodes[groupLink.TargetGroupID]; ok {
				// Sada popuni decu grupe (stvarni moduli)
				if groupDef := appConfig.GetModuleByID(groupLink.TargetGroupID); groupDef != nil && groupDef.SubModules != nil {
					for _, subModLink := range groupDef.SubModules {
						if actualModuleNode, ok := moduleNodes[subModLink.TargetModuleID]; ok {
							groupNode.Children = append(groupNode.Children, actualModuleNode)
						} else {
							log.Printf("WARNING: Target modul '%s' za submodul '%s' (u grupi '%s') nije pronađen. Možda nedostaje JSON fajl?", subModLink.TargetModuleID, subModLink.DisplayName, groupLink.TargetGroupID)
						}
					}
				}
				// Sortiraj decu unutar grupe ako ti je potrebno, npr. po display_order
				// groupNode.Children bi trebalo da se sortira ovde
				appRoot.Children = append(appRoot.Children, groupNode)
			} else {
				log.Printf("WARNING: Target grupa '%s' nije pronađena za grupu '%s' u root modulu. Možda nedostaje JSON fajl za grupu?", groupLink.TargetGroupID, groupLink.DisplayName)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(appRoot); err != nil {
		log.Printf("ERROR: Greška pri enkodiranju tree strukture modula: %v", err)
		http.Error(w, "Interna serverska greška pri vraćanju modula", http.StatusInternalServerError)
		return
	}
	log.Println("INFO: Vraćena tree struktura modula.")
}

// GetModuleRecords handles requests to get records for a specific module.
func GetModuleRecords(w http.ResponseWriter, req *http.Request, appConfig *AppConfig, dataset *SQLDataset) {
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]

	moduleDef := appConfig.GetModuleByID(moduleID)
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	records, err := dataset.GetRecords(moduleDef, req.URL.Query())
	if err != nil {
		http.Error(w, fmt.Sprintf("Greška pri dohvatanju zapisa za modul '%s': %v", moduleID, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(records); err != nil {
		http.Error(w, fmt.Sprintf("Greška pri enkodiranju zapisa: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("INFO: Vraćeno %d zapisa za modul '%s'.", len(records), moduleID)
}

// GetSingleRecord handles requests to get a single record by ID for a specific module.
func GetSingleRecord(w http.ResponseWriter, req *http.Request, appConfig *AppConfig, dataset *SQLDataset) {
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"] // Ovo će biti string, dataset.GetRecordByID očekuje interface{}

	moduleDef := appConfig.GetModuleByID(moduleID)
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	// Pokušaj da parsiraš recordID u int, ako je primarni ključ int
	// Inače ga prosledi kao string
	pkCol := dataset.getPrimaryKeyColumn(moduleDef)
	var parsedRecordID interface{}
	if pkCol != nil && pkCol.Type == "integer" {
		id, err := strconv.Atoi(recordID)
		if err != nil {
			http.Error(w, fmt.Sprintf("Nevažeći ID zapisa za modul '%s': %v", moduleID, err), http.StatusBadRequest)
			return
		}
		parsedRecordID = id
	} else {
		parsedRecordID = recordID
	}

	record, err := dataset.GetRecordByID(moduleDef, parsedRecordID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Greška pri dohvatanju zapisa sa ID '%s' za modul '%s': %v", recordID, moduleID, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(record); err != nil {
		http.Error(w, fmt.Sprintf("Greška pri enkodiranju zapisa: %v", err), http.StatusInternalServerError)
		return
	}
	log.Printf("INFO: Vraćen zapis sa ID '%v' za modul '%s'.", parsedRecordID, moduleID)
}

// CreateRecord handles requests to create a new record for a module.
func CreateRecord(w http.ResponseWriter, req *http.Request, appConfig *AppConfig, dataset *SQLDataset) {
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]

	moduleDef := appConfig.GetModuleByID(moduleID)
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Greška pri dekodiranju payload-a: %v", err), http.StatusBadRequest)
		return
	}

	// Validacija payload-a
	if err := validatePayload(payload, moduleDef.Columns, appConfig); err != nil {
		http.Error(w, fmt.Sprintf("Greška validacije payload-a: %v", err), http.StatusBadRequest)
		return
	}

	newID, err := dataset.CreateRecord(moduleDef, payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Greška pri kreiranju zapisa za modul '%s': %v", moduleID, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]interface{}{"message": "Zapis uspešno kreiran", "id": newID}); err != nil {
		log.Printf("ERROR: Greška pri enkodiranju odgovora za CreateRecord: %v", err)
		http.Error(w, "Interna serverska greška", http.StatusInternalServerError)
	}
	log.Printf("INFO: Kreiran zapis sa ID '%v' za modul '%s'.", newID, moduleID)
}

// UpdateRecord handles requests to update an existing record for a module.
func UpdateRecord(w http.ResponseWriter, req *http.Request, appConfig *AppConfig, dataset *SQLDataset) {
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"]

	moduleDef := appConfig.GetModuleByID(moduleID)
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Greška pri dekodiranju payload-a: %v", err), http.StatusBadRequest)
		return
	}

	// Validacija payload-a
	if err := validatePayload(payload, moduleDef.Columns, appConfig); err != nil {
		http.Error(w, fmt.Sprintf("Greška validacije payload-a: %v", err), http.StatusBadRequest)
		return
	}

	err := dataset.UpdateRecord(moduleDef, recordID, payload)
	if err != nil {
		http.Error(w, fmt.Sprintf("Greška pri ažuriranju zapisa sa ID '%s' za modul '%s': %v", recordID, moduleID, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"message": "Zapis uspešno ažuriran"}); err != nil {
		log.Printf("ERROR: Greška pri enkodiranju odgovora za UpdateRecord: %v", err)
		http.Error(w, "Interna serverska greška", http.StatusInternalServerError)
	}
	log.Printf("INFO: Ažuriran zapis sa ID '%s' za modul '%s'.", recordID, moduleID)
}

// DeleteRecord handles requests to delete an existing record for a module.
func DeleteRecord(w http.ResponseWriter, req *http.Request, appConfig *AppConfig, dataset *SQLDataset) {
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"]

	moduleDef := appConfig.GetModuleByID(moduleID)
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	err := dataset.DeleteRecord(moduleDef, recordID)
	if err != nil {
		http.Error(w, fmt.Sprintf("Greška pri brisanju zapisa sa ID '%s' za modul '%s': %v", recordID, moduleID, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"message": "Zapis uspešno obrisan"}); err != nil {
		log.Printf("ERROR: Greška pri enkodiranju odgovora za DeleteRecord: %v", err)
		http.Error(w, "Interna serverska greška", http.StatusInternalServerError)
	}
	log.Printf("INFO: Obrisan zapis sa ID '%s' za modul '%s'.", recordID, moduleID)
}
