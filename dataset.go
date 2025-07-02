// dataset.go
package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	_ "github.com/lib/pq" // PostgreSQL drajver
)

// SQLDataset handles database operations.
type SQLDataset struct {
	db     *sql.DB
	config *AppConfig // Dodato za pristup AppConfig i GetModuleByID
}

// NewSQLDataset creates a new SQLDataset instance.
func NewSQLDataset(config *AppConfig) (*SQLDataset, error) {
	connStr := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		config.GetDatabaseConfig().Host,
		config.GetDatabaseConfig().Port,
		config.GetDatabaseConfig().User,
		config.GetDatabaseConfig().Password,
		config.GetDatabaseConfig().DBName,
		config.GetDatabaseConfig().SSLMode,
	)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("greška pri otvaranju baze podataka: %w", err)
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("greška pri povezivanju sa bazom podataka: %w", err)
	}

	log.Println("INFO: Uspešno povezano sa bazom podataka.")
	return &SQLDataset{db: db, config: config}, nil
}

// Close closes the database connection.
func (s *SQLDataset) Close() {
	if s.db != nil {
		s.db.Close()
		log.Println("INFO: Veza sa bazom podataka zatvorena.")
	}
}

// GetRecords fetches records for a given module, applying filters, sorting, and pagination.
func (s *SQLDataset) GetRecords(moduleDef *ModuleDefinition, queryParams url.Values) ([]map[string]interface{}, error) {
	if moduleDef.DBTableName == "" && moduleDef.SelectQuery == "" {
		return nil, fmt.Errorf("modul '%s' nema definisanu tabelu ili select query", moduleDef.ID)
	}

	var baseQuery string
	if moduleDef.SelectQuery != "" {
		baseQuery = moduleDef.SelectQuery
	} else {
		// Konstruiši SELECT * FROM table_name ako SelectQuery nije definisan
		baseQuery = fmt.Sprintf("SELECT * FROM %s", moduleDef.DBTableName)
	}

	// Liste za SQL WHERE klauzulu i argumente za prepared statement
	whereClauses := []string{}
	args := []interface{}{}
	argCounter := 1 // Brojač za parametre ($1, $2, ...)

	// Limit i Offset
	limit := -1  // -1 znači bez limita
	offset := -1 // -1 znači bez offseta

	// Sortiranje
	orderByClauses := []string{}

	// Prođi kroz query parametre
	for key, values := range queryParams {
		if len(values) == 0 {
			continue
		}
		value := values[0] // Uzimamo samo prvu vrednost za svaki parametar

		switch key {
		case "_limit":
			if l, err := strconv.Atoi(value); err == nil && l >= 0 {
				limit = l
			} else {
				log.Printf("WARNING: Nevažeća vrednost za _limit: '%s'", value)
			}
		case "_offset":
			if o, err := strconv.Atoi(value); err == nil && o >= 0 {
				offset = o
			} else {
				log.Printf("WARNING: Nevažeća vrednost za _offset: '%s'", value)
			}
		case "_sort":
			// Primer: _sort=column1,-column2
			sortFields := strings.Split(value, ",")
			for _, field := range sortFields {
				field = strings.TrimSpace(field)
				if field == "" {
					continue
				}
				order := "ASC"
				columnName := field
				if strings.HasPrefix(field, "-") {
					order = "DESC"
					columnName = strings.TrimPrefix(field, "-")
				}
				// Proveri da li je kolona validna (da sprečimo SQL injection)
				if colDef := getColumnByDBName(moduleDef.Columns, columnName); colDef != nil {
					orderByClauses = append(orderByClauses, fmt.Sprintf("%s %s", colDef.DBColumnName, order))
				} else {
					log.Printf("WARNING: Pokušaj sortiranja po nepostojećoj koloni: '%s'", columnName)
				}
			}
		case "_search":
			// Pozovi pomoćnu funkciju za pretragu
			s.addSearchCondition(moduleDef, value, &whereClauses, &args, &argCounter)
		default:
			// Standardno filtriranje po kolonama (npr. 'column=value' ili 'column__gt=value')
			s.buildWhereClause(moduleDef, key, value, &whereClauses, &args, &argCounter)
		}
	}

	// Izgradnja finalnog SQL upita
	finalQuery := baseQuery

	if len(whereClauses) > 0 {
		finalQuery += " WHERE " + strings.Join(whereClauses, " AND ")
	}
	if len(orderByClauses) > 0 {
		finalQuery += " ORDER BY " + strings.Join(orderByClauses, ", ")
	}
	if limit != -1 {
		finalQuery += fmt.Sprintf(" LIMIT $%d", argCounter)
		args = append(args, limit)
		argCounter++
	}
	if offset != -1 {
		finalQuery += fmt.Sprintf(" OFFSET $%d", argCounter)
		args = append(args, offset)
		argCounter++
	}

	log.Printf("INFO: Izvršavanje SQL upita: %s sa parametrima: %v", finalQuery, args)

	rows, err := s.db.Query(finalQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("greška pri izvršavanju SELECT upita za modul '%s': %w", moduleDef.ID, err)
	}
	defer rows.Close()

	records := make([]map[string]interface{}, 0)
	columnNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatanju imena kolona: %w", err)
	}

	for rows.Next() {
		columns := make([]interface{}, len(columnNames))
		columnPointers := make([]interface{}, len(columnNames))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			return nil, fmt.Errorf("greška pri skeniranju reda: %w", err)
		}

		record := make(map[string]interface{})
		for i, colName := range columnNames {
			val := columns[i]
			// dbColDef := getColumnByDBName(moduleDef.Columns, colName) // Možda ti treba za tip
			if val == nil {
				record[colName] = nil
			} else {
				// PostgreSQL vraća neke tipove kao []byte, konvertujemo ih u string ako je to očekivano
				switch v := val.(type) {
				case []byte:
					record[colName] = string(v)
				default:
					record[colName] = v
				}
			}
		}

		records = append(records, record)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("greška nakon iteracije kroz redove: %w", err)
	}

	// Proširenje lookup i submodule polja
	if err := s.performLookupExpansion(records, moduleDef); err != nil {
		log.Printf("WARNING: Greška pri proširenju lookup-a za modul '%s': %v", moduleDef.ID, err)
		// Opcionalno: vrati grešku ili samo nastavi bez proširenja
	}

	if len(moduleDef.SubModules) > 0 {
		if pkCol := s.getPrimaryKeyColumn(moduleDef); pkCol != nil {
			for _, record := range records {
				if pkVal, ok := record[pkCol.DBColumnName]; ok {
					if err := s.performSubmoduleExpansion(record, moduleDef, pkVal); err != nil {
						log.Printf("WARNING: Greška pri proširenju submodula za modul '%s', PK '%v': %v", moduleDef.ID, pkVal, err)
						// Opcionalno: vrati grešku ili samo nastavi
					}
				}
			}
		} else {
			log.Printf("WARNING: Modul '%s' ima submodule ali nema definisan primarni ključ za proširenje.", moduleDef.ID)
		}
	}

	return records, nil
}

// GetReportData executes a select_query for report or custom type modules.
func (s *SQLDataset) GetReportData(moduleDef *ModuleDefinition, queryParams url.Values) ([]map[string]interface{}, error) {
	if moduleDef.SelectQuery == "" {
		return nil, fmt.Errorf("modul '%s' tipa '%s' nema definisan select_query", moduleDef.Name, moduleDef.Type)
	}

	query := moduleDef.SelectQuery

	if sortCol := queryParams.Get("sortBy"); sortCol != "" {
		sortOrder := queryParams.Get("sortOrder")
		if sortOrder == "" {
			sortOrder = "ASC"
		}
		query += fmt.Sprintf(" ORDER BY %s %s", sortCol, sortOrder)
	}

	log.Printf("DEBUG: Executing report query for '%s': %s", moduleDef.ID, query)
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("greška pri izvršavanju REPORT upita za modul '%s': %w", moduleDef.Name, err)
	}
	defer rows.Close()

	columnNames, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("greška pri dohvatanju imena kolona za izveštaj '%s': %w", moduleDef.Name, err)
	}

	var results []map[string]interface{}
	for rows.Next() {
		record := make(map[string]interface{})
		columnPointers := make([]interface{}, len(columnNames))
		columnValues := make([]interface{}, len(columnNames))

		for i := range columnNames {
			columnPointers[i] = &columnValues[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			return nil, fmt.Errorf("greška pri skeniranju reda izveštaja za modul '%s': %w", moduleDef.Name, err)
		}

		for i, colName := range columnNames {
			val := columnValues[i]
			if val == nil {
				record[colName] = nil
			} else {
				switch v := val.(type) {
				case []byte:
					record[colName] = string(v)
				default:
					record[colName] = v
				}
			}
		}
		results = append(results, record)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("greška nakon iteracije kroz redove izveštaja za modul '%s': %w", moduleDef.Name, err)
	}

	return results, nil
}

// getColumnByDBName je pomoćna funkcija za pronalaženje definicije kolone po DBColumnName
func getColumnByDBName(columns []ColumnDefinition, dbColumnName string) *ColumnDefinition {
	for i := range columns {
		if columns[i].DBColumnName == dbColumnName {
			return &columns[i]
		}
	}
	return nil
}

// buildWhereClause parsira filter parametre i dodaje ih u WHERE klauzulu.
func (s *SQLDataset) buildWhereClause(moduleDef *ModuleDefinition, key, value string, whereClauses *[]string, args *[]interface{}, argCounter *int) {
	parts := strings.Split(key, "__")
	columnName := parts[0]
	operator := ""
	if len(parts) > 1 {
		operator = parts[1]
	}

	colDef := getColumnByDBName(moduleDef.Columns, columnName)
	if colDef == nil {
		log.Printf("WARNING: Pokušaj filtriranja po nepostojećoj koloni: '%s'", columnName)
		return
	}

	sqlOperator := "="
	needsValueConversion := true
	needsLikeEscape := false

	switch operator {
	case "gt":
		sqlOperator = ">"
	case "gte":
		sqlOperator = ">="
	case "lt":
		sqlOperator = "<"
	case "lte":
		sqlOperator = "<="
	case "ne":
		sqlOperator = "!="
	case "like":
		sqlOperator = "LIKE"
		needsLikeEscape = true
	case "ilike":
		sqlOperator = "ILIKE" // Case-insensitive LIKE za PostgreSQL
		needsLikeEscape = true
	case "in":
		// Poseban tretman za IN operator: vrednosti se splituju i dodaju kao zasebni parametri
		vals := strings.Split(value, ",")
		placeholders := make([]string, len(vals))
		for i, v := range vals {
			placeholders[i] = fmt.Sprintf("$%d", *argCounter)
			convertedVal, err := convertValueToColumnType(v, colDef.Type)
			if err != nil {
				log.Printf("WARNING: Greška pri konverziji IN vrednosti za kolonu '%s': %v", colDef.Name, err)
				return
			}
			*args = append(*args, convertedVal)
			*argCounter++
		}
		*whereClauses = append(*whereClauses, fmt.Sprintf("%s %s (%s)", colDef.DBColumnName, sqlOperator, strings.Join(placeholders, ", ")))
		needsValueConversion = false // Vrednosti su već konvertovane
	default:
		// Ako operator nije eksplicitno naveden, pretpostavljamo '='
		sqlOperator = "="
	}

	if needsValueConversion {
		convertedVal, err := convertValueToColumnType(value, colDef.Type)
		if err != nil {
			log.Printf("WARNING: Greška pri konverziji vrednosti '%s' za kolonu '%s' (%s): %v", value, colDef.Name, colDef.Type, err)
			return
		}
		if needsLikeEscape {
			convertedVal = fmt.Sprintf("%%%s%%", convertedVal) // Dodaj % za LIKE/ILIKE pretragu po podstringu
		}
		*whereClauses = append(*whereClauses, fmt.Sprintf("%s %s $%d", colDef.DBColumnName, sqlOperator, *argCounter))
		*args = append(*args, convertedVal)
		*argCounter++
	}
}

// addSearchCondition dodaje uslov pretrage za "_search" parametar.
func (s *SQLDataset) addSearchCondition(moduleDef *ModuleDefinition, searchValue string, whereClauses *[]string, args *[]interface{}, argCounter *int) {
	searchableColumns := []string{}
	for _, colDef := range moduleDef.Columns {
		// Pretpostavljamo da su sve string kolone (koje su visible i editable) pretražive
		// Možeš dodati i novo polje "IsSearchable" u ColumnDefinition ako želiš veću kontrolu
		if colDef.Type == "string" && colDef.IsVisible { // i colDef.IsSearchable ako dodas
			searchableColumns = append(searchableColumns, colDef.DBColumnName)
		}
	}

	if len(searchableColumns) == 0 {
		log.Printf("WARNING: Modul '%s' nema definisane pretražive kolone za _search.", moduleDef.ID)
		return
	}

	searchParts := make([]string, len(searchableColumns))
	for i, colName := range searchableColumns {
		searchParts[i] = fmt.Sprintf("%s ILIKE $%d", colName, *argCounter)
	}

	*whereClauses = append(*whereClauses, fmt.Sprintf("(%s)", strings.Join(searchParts, " OR ")))
	*args = append(*args, fmt.Sprintf("%%%s%%", searchValue)) // Pretraga po podstringu
	*argCounter++
}

// convertValueToColumnType pokušava da konvertuje string vrednost u odgovarajući tip kolone.
func convertValueToColumnType(value string, colType string) (interface{}, error) {
	switch colType {
	case "integer":
		return strconv.Atoi(value)
	case "float":
		return strconv.ParseFloat(value, 64)
	case "boolean":
		return strconv.ParseBool(value)
	case "string", "text":
		return value, nil
	// Dodaj i druge tipove ako su ti potrebni (npr. "date", "datetime")
	default:
		return value, nil // Za nepoznate tipove, vrati string
	}
}

// getPrimaryKeyColumn vraća definiciju primarnog ključa za modul.
// (Ova funkcija je verovatno već u dataset.go ili models.go, ali je ostavljam ovde za kontekst)
func (s *SQLDataset) getPrimaryKeyColumn(moduleDef *ModuleDefinition) *ColumnDefinition {
	for _, col := range moduleDef.Columns {
		if col.IsPrimaryKey {
			return &col
		}
	}
	return nil
}

// CreateRecord inserts a new record into the database.
// Vraća (interface{}, error) jer vraća ID novog zapisa.
func (s *SQLDataset) CreateRecord(moduleDef *ModuleDefinition, payload map[string]interface{}) (interface{}, error) {
	if moduleDef.Type != "table" {
		return nil, fmt.Errorf("kreiranje zapisa nije podržano za modul tipa '%s'", moduleDef.Type)
	}

	cols := []string{}
	vals := []interface{}{}
	placeholders := []string{}

	i := 1
	for _, colDef := range moduleDef.Columns {
		// Preskoči kolone koje nisu editable, primarne ključeve i read-only
		if !colDef.IsEditable || colDef.IsPrimaryKey || colDef.IsReadOnly {
			continue
		}
		if val, ok := payload[colDef.DBColumnName]; ok {
			cols = append(cols, colDef.DBColumnName)
			vals = append(vals, val)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			i++
		} else if colDef.DefaultValue != nil {
			cols = append(cols, colDef.DBColumnName)
			vals = append(vals, colDef.DefaultValue)
			placeholders = append(placeholders, fmt.Sprintf("$%d", i))
			i++
		}
	}

	if len(cols) == 0 {
		// ISPRAVLJENO: Vraća (nil, error) da se poklopi sa definicijom funkcije
		return nil, fmt.Errorf("nema validnih polja za kreiranje zapisa u modulu '%s'", moduleDef.Name)
	}

	pkCol := s.getPrimaryKeyColumn(moduleDef)
	if pkCol == nil {
		// ISPRAVLJENO: Vraća (nil, error)
		return nil, fmt.Errorf("modul '%s' nema definisan primarni ključ za povratak ID-a", moduleDef.Name)
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) RETURNING %s",
		moduleDef.DBTableName,
		strings.Join(cols, ", "),
		strings.Join(placeholders, ", "),
		pkCol.DBColumnName,
	)

	log.Printf("DEBUG: Executing INSERT query: %s with values: %v", query, vals)

	var newID interface{}
	err := s.db.QueryRow(query, vals...).Scan(&newID)
	if err != nil {
		// ISPRAVLJENO: Vraća (nil, error)
		return nil, fmt.Errorf("greška pri izvršavanju INSERT upita za modul '%s': %w", moduleDef.Name, err)
	}

	return newID, nil
}

// UpdateRecord updates an existing record in the database.
// Vraća samo error.
func (s *SQLDataset) UpdateRecord(moduleDef *ModuleDefinition, recordID string, payload map[string]interface{}) error {
	if moduleDef.Type != "table" {
		return fmt.Errorf("ažuriranje zapisa nije podržano za modul tipa '%s'", moduleDef.Type)
	}

	setClauses := []string{}
	vals := []interface{}{}
	i := 1

	pkCol := s.getPrimaryKeyColumn(moduleDef)
	if pkCol == nil {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("modul '%s' nema definisan primarni ključ za ažuriranje", moduleDef.Name)
	}

	for _, colDef := range moduleDef.Columns {
		if !colDef.IsEditable || colDef.IsPrimaryKey || colDef.IsReadOnly {
			continue
		}
		if val, ok := payload[colDef.DBColumnName]; ok {
			setClauses = append(setClauses, fmt.Sprintf("%s = $%d", colDef.DBColumnName, i))
			vals = append(vals, val)
			i++
		}
	}

	if len(setClauses) == 0 {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("nema validnih polja za ažuriranje zapisa u modulu '%s'", moduleDef.Name)
	}

	// Dodaj recordID kao poslednji argument za WHERE klauzulu
	vals = append(vals, recordID)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE %s = $%d",
		moduleDef.DBTableName,
		strings.Join(setClauses, ", "),
		pkCol.DBColumnName, i, // i je sada indeks poslednjeg placeholder-a ($d)
	)

	log.Printf("DEBUG: Executing UPDATE query: %s with values: %v", query, vals)

	res, err := s.db.Exec(query, vals...)
	if err != nil {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("greška pri izvršavanju UPDATE upita za modul '%s', ID '%s': %w", moduleDef.Name, recordID, err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("zapis sa ID '%s' nije pronađen ili ažuriran u modulu '%s'", recordID, moduleDef.Name)
	}

	return nil
}

// DeleteRecord deletes a record from the database.
// Vraća samo error.
func (s *SQLDataset) DeleteRecord(moduleDef *ModuleDefinition, recordID string) error {
	if moduleDef.Type != "table" {
		return fmt.Errorf("brisanje zapisa nije podržano za modul tipa '%s'", moduleDef.Type)
	}

	pkCol := s.getPrimaryKeyColumn(moduleDef)
	if pkCol == nil {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("modul '%s' nema definisan primarni ključ za brisanje", moduleDef.Name)
	}

	query := fmt.Sprintf("DELETE FROM %s WHERE %s = $1",
		moduleDef.DBTableName,
		pkCol.DBColumnName,
	)

	log.Printf("DEBUG: Executing DELETE query: %s with ID: %s", query, recordID)

	res, err := s.db.Exec(query, recordID)
	if err != nil {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("greška pri izvršavanju DELETE upita za modul '%s', ID '%s': %w", moduleDef.Name, recordID, err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		// ISPRAVLJENO: Vraća samo error
		return fmt.Errorf("zapis sa ID '%s' nije pronađen ili obrisan u modulu '%s'", recordID, moduleDef.Name)
	}

	return nil
}

// GetRecordByID fetches a single record by its ID.
// This is used by GetSingleRecord in app.go
func (s *SQLDataset) GetRecordByID(moduleDef *ModuleDefinition, id interface{}) (map[string]interface{}, error) {
	pkCol := s.getPrimaryKeyColumn(moduleDef)
	if pkCol == nil {
		return nil, fmt.Errorf("modul '%s' nema definisan primarni ključ", moduleDef.Name)
	}

	columns := getVisibleDBColumnNames(moduleDef.Columns) // Koristimo pomoćnu funkciju
	if len(columns) == 0 {
		return nil, fmt.Errorf("modul '%s' nema definisanih vidljivih kolona za dohvatanje zapisa po ID-u", moduleDef.Name)
	}

	// Kreiramo SELECT klauzulu sa aliasingom za svaku kolonu
	selectColumns := make([]string, len(columns))
	for i, colName := range columns {
		selectColumns[i] = fmt.Sprintf("%s AS %s", colName, colName)
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
		strings.Join(selectColumns, ", "),
		moduleDef.DBTableName,
		pkCol.DBColumnName,
	)

	log.Printf("DEBUG: Executing GetRecordByID query: %s with ID: %v", query, id)

	row := s.db.QueryRow(query, id)

	record := make(map[string]interface{})

	// Kreiramo dinamičke "destinacije" za Scan na osnovu vidljivih kolona
	// To osigurava da se slaže broj skeniranih kolona sa brojem kolona u upitu
	columnValues := make([]interface{}, len(columns))
	columnPointers := make([]interface{}, len(columns))
	for i := range columns {
		columnPointers[i] = &columnValues[i]
	}

	if err := row.Scan(columnPointers...); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("zapis sa ID '%v' nije pronađen u modulu '%s'", id, moduleDef.Name)
		}
		return nil, fmt.Errorf("greška pri skeniranju pojedinačnog reda: %w", err)
	}

	// Mapiramo skenirane vrednosti na mapu, koristeći dbColumnNames iz 'columns' slice-a
	for i, dbColName := range columns {
		val := columnValues[i]
		if val == nil {
			record[dbColName] = nil
		} else {
			switch v := val.(type) {
			case []byte:
				record[dbColName] = string(v)
			default:
				record[dbColName] = v
			}
		}
	}

	// Perform lookup expansion for this single record
	if err := s.performLookupExpansion([]map[string]interface{}{record}, moduleDef); err != nil {
		log.Printf("WARNING: Greška pri proširenju lookup-a za pojedinačni zapis u modulu '%s': %v", moduleDef.ID, err)
	}

	// Perform submodule expansion
	if len(moduleDef.SubModules) > 0 {
		// PK je već poznat kao id
		if err := s.performSubmoduleExpansion(record, moduleDef, id); err != nil {
			log.Printf("WARNING: Greška pri proširenju submodula za pojedinačni zapis '%v': %v", id, err)
		}
	}

	return record, nil
}

// getVisibleDBColumnNames helper to get only visible DB column names for SELECT query
func getVisibleDBColumnNames(cols []ColumnDefinition) []string {
	visibleCols := make([]string, 0)
	for _, col := range cols {
		// Dodaj DBColumnName samo ako je kolona vidljiva
		if col.IsVisible {
			visibleCols = append(visibleCols, col.DBColumnName)
		}
	}
	return visibleCols
}

// performLookupExpansion is now internal and part of GetRecords/GetRecordByID flow
func (s *SQLDataset) performLookupExpansion(records []map[string]interface{}, currentModule *ModuleDefinition) error {
	for _, colDef := range currentModule.Columns {
		// Proveri da li je kolona lookup tipa i da li ima definisan modul za lookup
		if colDef.Type == "lookup" && colDef.LookupModule != nil && colDef.LookupModuleID != "" {
			lookupModule := colDef.LookupModule
			lookupPKCol := s.getPrimaryKeyColumn(lookupModule)
			if lookupPKCol == nil {
				log.Printf("WARNING: Lookup modul '%s' za kolonu '%s' nema definisan primarni ključ, preskačem proširenje", lookupModule.ID, colDef.Name)
				continue
			}

			// Sakupi sve jedinstvene lookup ID-eve iz trenutnih zapisa
			lookupIDs := make(map[interface{}]struct{})
			for _, record := range records {
				if id, ok := record[colDef.DBColumnName]; ok && id != nil {
					lookupIDs[id] = struct{}{}
				}
			}

			if len(lookupIDs) == 0 {
				continue // Nema lookup ID-eva za obradu u ovoj koloni
			}

			// Pripremi WHERE klauzulu za batch dohvatanje lookup podataka
			placeholders := make([]string, 0, len(lookupIDs))
			args := make([]interface{}, 0, len(lookupIDs))
			paramCounter := 1
			for id := range lookupIDs {
				placeholders = append(placeholders, fmt.Sprintf("$%d", paramCounter))
				args = append(args, id)
				paramCounter++
			}

			// Odaberi kolone za lookup. Koristi LookupDisplayField ako je definisan
			lookupColsToSelect := []string{lookupPKCol.DBColumnName}
			lookupDisplayCol := ""

			if colDef.LookupDisplayField != "" { // Koristi LookupDisplayField
				lookupDisplayCol = colDef.LookupDisplayField
			} else {
				// Fallback na prvu string kolonu (ili "name" ako postoji)
				for _, lc := range lookupModule.Columns {
					if lc.DBColumnName == "name" && lc.Type == "string" {
						lookupDisplayCol = "name"
						break
					}
				}
				if lookupDisplayCol == "" {
					for _, lc := range lookupModule.Columns {
						if lc.DBColumnName != lookupPKCol.DBColumnName && lc.Type == "string" {
							lookupDisplayCol = lc.DBColumnName
							break
						}
					}
				}
				if lookupDisplayCol == "" {
					lookupDisplayCol = lookupPKCol.DBColumnName // Fallback na ID ako nema string kolone
				}
			}

			// Dodaj prikaznu kolonu u SELECT listu, ako već nije primarni ključ
			if lookupDisplayCol != lookupPKCol.DBColumnName {
				lookupColsToSelect = append(lookupColsToSelect, lookupDisplayCol)
			}

			lookupQuery := fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)",
				strings.Join(lookupColsToSelect, ", "),
				lookupModule.DBTableName,
				lookupPKCol.DBColumnName,
				strings.Join(placeholders, ", "),
			)

			lookupRows, err := s.db.Query(lookupQuery, args...)
			if err != nil {
				return fmt.Errorf("greška pri dohvatanju lookup podataka za kolonu '%s': %w", colDef.Name, err)
			}
			defer lookupRows.Close()

			// Mapiraj lookup ID-eve na dohvataene objekte
			lookupMap := make(map[interface{}]map[string]interface{})
			for lookupRows.Next() {
				lookupCols, err := lookupRows.Columns()
				if err != nil {
					return fmt.Errorf("greška pri čitanju naziva kolona lookup-a: %w", err)
				}
				values := make([]interface{}, len(lookupCols))
				pointers := make([]interface{}, len(lookupCols))
				for i := range values {
					pointers[i] = &values[i]
				}

				err = lookupRows.Scan(pointers...)
				if err != nil {
					return fmt.Errorf("greška pri skeniranju lookup reda: %w", err)
				}

				lookupRecord := make(map[string]interface{})
				for i, colName := range lookupCols {
					val := values[i]
					if b, ok := val.([]byte); ok {
						val = string(b)
					}
					lookupRecord[colName] = val
				}
				if id, ok := lookupRecord[lookupPKCol.DBColumnName]; ok {
					lookupMap[id] = lookupRecord
				}
			}
			if err = lookupRows.Err(); err != nil {
				return fmt.Errorf("greška nakon iteracije lookup redova: %w", err)
			}

			// Ažuriraj originalne zapise sa proširenim lookup podacima
			for idx := range records {
				record := records[idx]
				if id, ok := record[colDef.DBColumnName]; ok && id != nil {
					if expandedVal, found := lookupMap[id]; found {
						lookupObject := map[string]interface{}{
							"id": id,
						}
						if val, ok := expandedVal[lookupDisplayCol]; ok {
							lookupObject["name"] = val
						} else {
							lookupObject["name"] = fmt.Sprintf("ID: %v", id)
						}
						records[idx][colDef.DBColumnName] = lookupObject
					} else {
						records[idx][colDef.DBColumnName] = nil
					}
				}
			}
		}
	}
	return nil
}

// performSubmoduleExpansion fetches and attaches submodule data to a parent record.
func (s *SQLDataset) performSubmoduleExpansion(parentRecord map[string]interface{}, parentModuleDef *ModuleDefinition, parentPKVal interface{}) error {
	for _, subModDef := range parentModuleDef.SubModules {
		targetModule := s.config.GetModuleByID(subModDef.TargetModuleID)
		if targetModule == nil {
			log.Printf("WARNING: Target modul '%s' za submodul '%s' nije pronađen.", subModDef.TargetModuleID, subModDef.DisplayName)
			continue
		}

		columns := getVisibleDBColumnNames(targetModule.Columns) // Koristi pomoćnu funkciju i ovde
		if len(columns) == 0 {
			log.Printf("WARNING: Submodul '%s' (modul '%s') nema definisanih vidljivih kolona.", subModDef.DisplayName, targetModule.ID)
			continue
		}

		// Kreiramo SELECT klauzulu sa aliasingom za svaku kolonu
		selectColumns := make([]string, len(columns))
		for i, colName := range columns {
			selectColumns[i] = fmt.Sprintf("%s AS %s", colName, colName)
		}

		query := fmt.Sprintf("SELECT %s FROM %s WHERE %s = $1",
			strings.Join(selectColumns, ", "),
			targetModule.DBTableName,
			subModDef.ChildForeignKeyField,
		)

		log.Printf("DEBUG: Executing submodule query for '%s': %s with parent PK: %v", subModDef.DisplayName, query, parentPKVal)

		rows, err := s.db.Query(query, parentPKVal)
		if err != nil {
			return fmt.Errorf("greška pri dohvatanju podataka za submodul '%s': %w", subModDef.DisplayName, err)
		}
		defer rows.Close()

		var subRecords []map[string]interface{}
		for rows.Next() {
			subRecord := make(map[string]interface{})

			// Dohvati stvarne nazive kolona iz baze za submodul
			dbColumnNames, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("greška pri dohvatanju naziva kolona submodula iz baze: %w", err)
			}

			columnValues := make([]interface{}, len(dbColumnNames))
			columnPointers := make([]interface{}, len(dbColumnNames))
			for i := range dbColumnNames {
				columnPointers[i] = &columnValues[i]
			}

			if err := rows.Scan(columnPointers...); err != nil {
				return fmt.Errorf("greška pri skeniranju reda submodula '%s': %w", subModDef.DisplayName, err)
			}

			for i, dbColName := range dbColumnNames {
				val := columnValues[i]
				if val == nil {
					subRecord[dbColName] = nil
				} else {
					switch v := val.(type) {
					case []byte:
						subRecord[dbColName] = string(v)
					default:
						subRecord[dbColName] = v
					}
				}
			}
			if err := s.performLookupExpansion([]map[string]interface{}{subRecord}, targetModule); err != nil {
				log.Printf("WARNING: Greška pri proširenju lookup-a u submodulu '%s': %v", subModDef.DisplayName, err)
			}
			if len(targetModule.SubModules) > 0 {
				if subPKCol := s.getPrimaryKeyColumn(targetModule); subPKCol != nil {
					if subPKVal, ok := subRecord[subPKCol.DBColumnName]; ok {
						if err := s.performSubmoduleExpansion(subRecord, targetModule, subPKVal); err != nil {
							log.Printf("WARNING: Greška pri rekurzivnom proširenju submodula '%s' unutar '%s': %v", subModDef.DisplayName, parentModuleDef.ID, err)
						}
					}
				}
			}
			subRecords = append(subRecords, subRecord)
		}

		if err = rows.Err(); err != nil {
			return fmt.Errorf("greška nakon iteracije kroz redove submodula '%s': %w", subModDef.DisplayName, err)
		}

		parentRecord[subModDef.TargetModuleID] = subRecords
	}
	return nil
}
