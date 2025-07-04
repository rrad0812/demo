// api.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv" // Potrebno za strconv.Atoi
	"time"

	"github.com/gorilla/mux"
)

// APIServer sadrži zavisnosti za API handlere.
type APIServer struct {
	config  *AppConfig
	dataset *SQLDataset
	router  *mux.Router
}

// NewAPIServer kreira novu instancu APIServer-a.
func NewAPIServer(config *AppConfig, dataset *SQLDataset) *APIServer {
	s := &APIServer{
		config:  config,
		dataset: dataset,
		router:  mux.NewRouter(),
	}
	s.InitRoutes() // Inicijalizuj rute odmah po kreiranju servera
	return s
}

// InitRoutes inicijalizuje sve API rute.
func (s *APIServer) InitRoutes() {
	s.router.HandleFunc("/api/modules", s.GetAllModules).Methods("GET")
	s.router.HandleFunc("/api/modules/{moduleID}", s.GetModuleRecords).Methods("GET")
	s.router.HandleFunc("/api/modules/{moduleID}/{recordID}", s.GetSingleRecord).Methods("GET")
	s.router.HandleFunc("/api/modules/{moduleID}", s.CreateRecord).Methods("POST")
	s.router.HandleFunc("/api/modules/{moduleID}/{recordID}", s.UpdateRecord).Methods("PUT")
	s.router.HandleFunc("/api/modules/{moduleID}/{recordID}", s.DeleteRecord).Methods("DELETE")
}

// Start pokreće HTTP server.
func (s *APIServer) Start(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.router, // Koristimo serverov router kao handler
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("INFO: Server pokrenut na adresi %s", addr)
	return srv.ListenAndServe()
}

// GetAllModules handles requests to get all module definitions in a hierarchical (tree) structure for UI.
func (s *APIServer) GetAllModules(w http.ResponseWriter, req *http.Request) {
	type UINode struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		Type     string   `json:"type"`
		Children []UINode `json:"children,omitempty"`
		Icon     string   `json:"icon,omitempty"`
	}

	var appRoot *UINode = nil
	groupNodes := make(map[string]UINode)
	moduleNodes := make(map[string]UINode)

	for _, moduleDef := range s.config.Modules { // Koristimo s.config
		node := UINode{
			ID:   moduleDef.ID,
			Name: moduleDef.Name,
			Type: moduleDef.Type,
		}

		if moduleDef.Type == "root" {
			appRoot = &node
		} else if moduleDef.Type == "group" {
			groupNodes[moduleDef.ID] = node
		} else {
			moduleNodes[moduleDef.ID] = node
		}
	}

	if appRoot == nil {
		appRoot = &UINode{
			ID:   "app_root",
			Name: "Aplikacija",
			Type: "root",
		}
	}

	if appDef := s.config.GetModuleByID("app"); appDef != nil && appDef.Groups != nil { // Koristimo s.config
		for _, groupLink := range appDef.Groups {
			if groupNode, ok := groupNodes[groupLink.TargetGroupID]; ok {
				if groupDef := s.config.GetModuleByID(groupLink.TargetGroupID); groupDef != nil && groupDef.SubModules != nil { // Koristimo s.config
					for _, subModLink := range groupDef.SubModules {
						if actualModuleNode, ok := moduleNodes[subModLink.TargetModuleID]; ok {
							groupNode.Children = append(groupNode.Children, actualModuleNode)
						} else {
							log.Printf("WARNING: Target modul '%s' za submodul '%s' (u grupi '%s') nije pronađen. Možda nedostaje JSON fajl?", subModLink.TargetModuleID, subModLink.DisplayName, groupLink.TargetGroupID)
						}
					}
				}
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
func (s *APIServer) GetModuleRecords(w http.ResponseWriter, req *http.Request) { // Metoda APIServera
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]

	moduleDef := s.config.GetModuleByID(moduleID) // Koristimo s.config
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	records, err := s.dataset.GetRecords(moduleDef, req.URL.Query()) // Koristimo s.dataset
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
func (s *APIServer) GetSingleRecord(w http.ResponseWriter, req *http.Request) { // Metoda APIServera
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"]

	moduleDef := s.config.GetModuleByID(moduleID) // Koristimo s.config
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	pkCol := s.dataset.getPrimaryKeyColumn(moduleDef) // Koristimo s.dataset
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

	record, err := s.dataset.GetRecordByID(moduleDef, parsedRecordID) // Koristimo s.dataset
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
func (s *APIServer) CreateRecord(w http.ResponseWriter, req *http.Request) { // Metoda APIServera
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]

	moduleDef := s.config.GetModuleByID(moduleID) // Koristimo s.config
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("Greška pri dekodiranju payload-a: %v", err), http.StatusBadRequest)
		return
	}

	// Validacija payload-a - validatePayload je i dalje samostalna funkcija, ali joj prosleđujemo s.config
	if err := validatePayload(payload, moduleDef.Columns, s.config); err != nil {
		http.Error(w, fmt.Sprintf("Greška validacije payload-a: %v", err), http.StatusBadRequest)
		return
	}

	newID, err := s.dataset.CreateRecord(moduleDef, payload) // Koristimo s.dataset
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
func (s *APIServer) UpdateRecord(w http.ResponseWriter, req *http.Request) { // Metoda APIServera
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"]

	moduleDef := s.config.GetModuleByID(moduleID) // Koristimo s.config
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
	if err := validatePayload(payload, moduleDef.Columns, s.config); err != nil {
		http.Error(w, fmt.Sprintf("Greška validacije payload-a: %v", err), http.StatusBadRequest)
		return
	}

	err := s.dataset.UpdateRecord(moduleDef, recordID, payload) // Koristimo s.dataset
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
func (s *APIServer) DeleteRecord(w http.ResponseWriter, req *http.Request) { // Metoda APIServera
	vars := mux.Vars(req)
	moduleID := vars["moduleID"]
	recordID := vars["recordID"]

	moduleDef := s.config.GetModuleByID(moduleID) // Koristimo s.config
	if moduleDef == nil {
		http.Error(w, fmt.Sprintf("Modul sa ID '%s' nije pronađen.", moduleID), http.StatusNotFound)
		return
	}

	err := s.dataset.DeleteRecord(moduleDef, recordID) // Koristimo s.dataset
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
