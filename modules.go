// models.go
package main

// ColumnDefinition defines the structure of a column in a module.
type ColumnDefinition struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Type               string      `json:"type"` // e.g., "string", "integer", "float", "boolean", "date", "datetime", "lookup"
	DBColumnName       string      `json:"db_column_name"`
	IsPrimaryKey       bool        `json:"is_primary_key"`
	IsSearchable       bool        `json:"is_searchable"`
	IsSortable         bool        `json:"is_sortable"`
	IsVisible          bool        `json:"is_visible"`
	IsEditable         bool        `json:"is_editable"`
	IsReadOnly         bool        `json:"is_read_only"`         // Dodato za read-only polja (npr. auto-increment ID)
	Validation         string      `json:"validation"`           // e.g., "required,min:5,max:100,email,regex:^[A-Za-z]+$"
	DefaultValue       interface{} `json:"default_value"`        // Defaultna vrednost za kreiranje
	LookupModuleID     string      `json:"lookup_module_id"`     // ID modula za lookup polja
	LookupDisplayField string      `json:"lookup_display_field"` // Polje iz lookup modula koje se prikazuje
	// Runtime fields (populated during app initialization)
	LookupModule *ModuleDefinition `json:"-"` // Pointer to the actual ModuleDefinition for lookup
}

// SubModuleDefinition defines a submodule relationship.
type SubModuleDefinition struct {
	ID                   string `json:"id"`
	DisplayName          string `json:"display_name"`
	TargetModuleID       string `json:"target_module_id"`        // ID modula koji predstavlja submodule
	ChildForeignKeyField string `json:"child_foreign_key_field"` // Polje u target modulu koje referencira primarni ključ roditelja
	// Runtime fields
	TargetModule *ModuleDefinition `json:"-"` // Pointer to the actual ModuleDefinition for the target module
}

// ModuleDefinition defines the structure of a data module.
type ModuleDefinition struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"` // e.g., "table", "group", "root", "report", "custom"
	Description  string                 `json:"description"`
	DBTableName  string                 `json:"db_table_name"` // Used for "table" type modules
	DisplayField string                 `json:"display_field"` // Field to display in lists (e.g., "name" or "title")
	SelectQuery  string                 `json:"select_query"`  // Used for "report" or "custom" type modules
	Columns      []ColumnDefinition     `json:"columns"`
	SubModules   []SubModuleDefinition  `json:"sub_modules"`
	Properties   map[string]interface{} `json:"properties,omitempty"` // Dodaj ako već nema
	Groups       []GroupLink            `json:"groups,omitempty"`     // <-- NOVO: Dodaj ovo polje za "app" modul
}

// GroupLink defines a link to a group, used within the root module (e.g., app.json)
type GroupLink struct {
	TargetGroupID string `json:"target_group_id"`
	Type          string `json:"type"` // e.g., "group" or "group_link"
	DisplayName   string `json:"display_name"`
	DisplayOrder  int    `json:"display_order"`
}
