// validation.go
package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	// "regexp" // Više nije direktno potreban ovde, jer koristimo config.GetCompiledRegex
)

// validatePayload validates the incoming JSON payload against module column definitions.
// Ova funkcija sada prima *AppConfig kao 'config' argument.
func validatePayload(payload map[string]interface{}, columns []ColumnDefinition, config *AppConfig) error {
	for _, colDef := range columns {
		// Preskoči kolone koje nisu editable (npr. automatski generisani ID-evi)
		// i primarne ključeve ako nisu deo payload-a (ili ako se ne očekuje da ih klijent šalje za kreiranje)
		// Bitno je da se primarni ključ validira SAMO ako je poslat.
		// Ako je IsReadOnly true, ta kolona se ne može menjati, pa je preskačemo za validaciju payload-a.
		if colDef.IsReadOnly {
			continue
		}

		val, exists := payload[colDef.DBColumnName] // Validira po DBColumnName, a ne po ID-u kolone

		// --- Provera obaveznih polja (required) ---
		// Ako polje ne postoji u payload-u i obavezno je
		if !exists && strings.Contains(colDef.Validation, "required") {
			return fmt.Errorf("polje '%s' (DB kolona: %s) je obavezno", colDef.Name, colDef.DBColumnName)
		}
		// Ako polje postoji, ali je nil (JSON null), i obavezno je
		if exists && val == nil && strings.Contains(colDef.Validation, "required") {
			return fmt.Errorf("polje '%s' (DB kolona: %s) je obavezno i ne može biti prazno", colDef.Name, colDef.DBColumnName)
		}

		// Ako polje ne postoji ili je nil, a NIJE obavezno, preskoči dalju validaciju za to polje
		if !exists || val == nil {
			continue
		}

		// --- Validacija tipa (ako vrednost postoji i nije nil) ---
		switch colDef.Type {
		case "string":
			if _, ok := val.(string); !ok {
				return fmt.Errorf("polje '%s' (DB kolona: %s) mora biti string (primljen tip: %T)", colDef.Name, colDef.DBColumnName, val)
			}
		case "integer":
			// JSON unmarshals brojeve kao float64 po defaultu
			if vFloat, ok := val.(float64); !ok {
				return fmt.Errorf("polje '%s' (DB kolona: %s) mora biti ceo broj (primljen tip: %T)", colDef.Name, colDef.DBColumnName, val)
			} else if vFloat != float64(int(vFloat)) { // Provera da li je float64 zapravo ceo broj
				return fmt.Errorf("polje '%s' (DB kolona: %s) mora biti ceo broj (nije decimalni)", colDef.Name, colDef.DBColumnName)
			}
		case "float":
			if _, ok := val.(float64); !ok {
				return fmt.Errorf("polje '%s' (DB kolona: %s) mora biti decimalni broj (primljen tip: %T)", colDef.Name, colDef.DBColumnName, val)
			}
		case "boolean":
			if _, ok := val.(bool); !ok {
				return fmt.Errorf("polje '%s' (DB kolona: %s) mora biti logička vrednost (true/false) (primljen tip: %T)", colDef.Name, colDef.DBColumnName, val)
			}
		// TODO: Dodaj još provera tipova za date, datetime, itd.
		default:
			// Ako tip nije eksplicitno obrađen, loguj upozorenje ili ga preskoči
			log.Printf("INFO: Tip kolone '%s' ('%s') nije eksplicitno obrađen u validaciji. Primljen tip: %T", colDef.Name, colDef.Type, val)
		}

		// --- Pravila validacije (min, max, regex, email, itd.) ---
		if colDef.Validation != "" {
			rules := strings.Split(colDef.Validation, ",")
			for _, rule := range rules {
				rule = strings.TrimSpace(rule)
				// "required" je već obrađen iznad
				if rule == "required" {
					continue
				} else if strings.HasPrefix(rule, "min:") {
					minValStr := strings.TrimPrefix(rule, "min:")
					minVal, err := strconv.ParseFloat(minValStr, 64)
					if err != nil {
						log.Printf("ERROR: Greška pri parsiranju min validacije '%s' za '%s': %v", minValStr, colDef.Name, err)
						// Ne prekidaj izvršenje, samo preskoči ovo pravilo
						continue
					}
					// Provera za brojeve
					if vFloat, ok := val.(float64); ok && vFloat < minVal {
						return fmt.Errorf("polje '%s' mora biti najmanje %.2f", colDef.Name, minVal)
					}
					// Provera za stringove (dužina)
					if vStr, ok := val.(string); ok && float64(len(vStr)) < minVal {
						return fmt.Errorf("polje '%s' mora imati najmanje %d karaktera", colDef.Name, int(minVal))
					}
				} else if strings.HasPrefix(rule, "max:") {
					maxValStr := strings.TrimPrefix(rule, "max:")
					maxVal, err := strconv.ParseFloat(maxValStr, 64)
					if err != nil {
						log.Printf("ERROR: Greška pri parsiranju max validacije '%s' za '%s': %v", maxValStr, colDef.Name, err)
						continue
					}
					// Provera za brojeve
					if vFloat, ok := val.(float64); ok && vFloat > maxVal {
						return fmt.Errorf("polje '%s' može biti najviše %.2f", colDef.Name, maxVal)
					}
					// Provera za stringove (dužina)
					if vStr, ok := val.(string); ok && float64(len(vStr)) > maxVal {
						return fmt.Errorf("polje '%s' može imati najviše %d karaktera", colDef.Name, int(maxVal))
					}
				} else if rule == "email" {
					if vStr, ok := val.(string); ok {
						// ISPRAVKA: Koristimo prekompilirani email regex
						re, found := config.GetCompiledRegex("emailValidation") // Koristimo ključ "emailValidation"
						if !found {
							log.Printf("ERROR: Kompilirani email regex nije pronađen. Proverite AppConfig.CompileRegexes().")
							// Ako regex nije pronađen, nastavi, ali loguj upozorenje.
							continue
						}
						if !re.MatchString(vStr) {
							return fmt.Errorf("polje '%s' mora biti validna email adresa", colDef.Name)
						}
					}
				} else if strings.HasPrefix(rule, "regex:") {
					pattern := strings.TrimPrefix(rule, "regex:")
					if vStr, ok := val.(string); ok {
						re, found := config.GetCompiledRegex(pattern) // Koristi pattern kao ključ
						if !found {
							log.Printf("ERROR: Regex '%s' nije kompilovan za kolonu '%s'. Proverite config.go CompileRegexes.", pattern, colDef.Name)
							continue
						}
						if !re.MatchString(vStr) {
							return fmt.Errorf("polje '%s' ne ispunjava zahtevani format (regex: %s)", colDef.Name, pattern)
						}
					}
				}
				// TODO: Dodaj još pravila validacije po potrebi
			}
		}
	}
	return nil
}
