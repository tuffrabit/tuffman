package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

const schema = `
CREATE TABLE IF NOT EXISTS files (
    id TEXT PRIMARY KEY,
    absolute_path TEXT NOT NULL,
    language TEXT,
    size_bytes INTEGER,
    mtime INTEGER,
    indexed_at INTEGER,
    git_sha TEXT
);

CREATE TABLE IF NOT EXISTS symbols (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    language TEXT,
    kind TEXT NOT NULL,
    name TEXT NOT NULL,
    signature TEXT,
    doc TEXT,
    line_start INTEGER,
    line_end INTEGER,
    receiver TEXT,
    FOREIGN KEY (file_id) REFERENCES files(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);

CREATE TABLE IF NOT EXISTS symbol_refs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id TEXT NOT NULL,
    target_name TEXT NOT NULL,
    target_id TEXT,
    kind TEXT,
    line INTEGER,
    FOREIGN KEY (source_id) REFERENCES symbols(id) ON DELETE CASCADE,
    FOREIGN KEY (target_id) REFERENCES symbols(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_refs_source ON symbol_refs(source_id);
CREATE INDEX IF NOT EXISTS idx_refs_target_name ON symbol_refs(target_name);
CREATE INDEX IF NOT EXISTS idx_refs_target_id ON symbol_refs(target_id);
`

// DB wraps the SQLite database connection
type DB struct {
	conn *sql.DB
	path string
}

// File represents a source file in the database
type File struct {
	ID           string
	AbsolutePath string
	Language     string
	SizeBytes    int64
	Mtime        int64
	IndexedAt    int64
	GitSHA       string
}

// Symbol represents a code symbol (function, struct, etc.) in the database
type Symbol struct {
	ID        string
	FileID    string
	Language  string
	Kind      string
	Name      string
	Signature string
	Doc       string
	LineStart int
	LineEnd   int
	Receiver  string
}

// Reference represents a reference from one symbol to another
type Reference struct {
	ID         int64
	SourceID   string
	TargetName string
	TargetID   *string // Nullable - may be unresolved
	Kind       string  // "call", "import"
	Line       int
}

// Open opens or creates the SQLite database at the given path.
// It initializes the schema if the database is new.
func Open(dbPath string) (*DB, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	db := &DB{conn: conn, path: dbPath}

	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrating database: %w", err)
	}

	return db, nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.conn.Close()
}

// migrate initializes the database schema
func (db *DB) migrate() error {
	_, err := db.conn.Exec(schema)
	return err
}

// SaveFile inserts or replaces a file record
func (db *DB) SaveFile(file *File) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO files (id, absolute_path, language, size_bytes, mtime, indexed_at, git_sha)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, file.ID, file.AbsolutePath, file.Language, file.SizeBytes, file.Mtime, file.IndexedAt, file.GitSHA)
	return err
}

// GetFile retrieves a file by ID
func (db *DB) GetFile(id string) (*File, error) {
	row := db.conn.QueryRow(`
		SELECT id, absolute_path, language, size_bytes, mtime, indexed_at, git_sha
		FROM files WHERE id = ?
	`, id)

	var f File
	err := row.Scan(&f.ID, &f.AbsolutePath, &f.Language, &f.SizeBytes, &f.Mtime, &f.IndexedAt, &f.GitSHA)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// DeleteFile removes a file and its symbols (cascading delete)
func (db *DB) DeleteFile(id string) error {
	_, err := db.conn.Exec(`DELETE FROM files WHERE id = ?`, id)
	return err
}

// DeleteFileByAbsolutePath removes a file by its absolute path
func (db *DB) DeleteFileByAbsolutePath(absPath string) error {
	_, err := db.conn.Exec(`DELETE FROM files WHERE absolute_path = ?`, absPath)
	return err
}

// GetFileByAbsolutePath retrieves a file by its absolute path
func (db *DB) GetFileByAbsolutePath(absPath string) (*File, error) {
	row := db.conn.QueryRow(`
		SELECT id, absolute_path, language, size_bytes, mtime, indexed_at, git_sha
		FROM files WHERE absolute_path = ?
	`, absPath)

	var f File
	err := row.Scan(&f.ID, &f.AbsolutePath, &f.Language, &f.SizeBytes, &f.Mtime, &f.IndexedAt, &f.GitSHA)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}

// SaveSymbol inserts or replaces a symbol record
func (db *DB) SaveSymbol(sym *Symbol) error {
	_, err := db.conn.Exec(`
		INSERT OR REPLACE INTO symbols (id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sym.ID, sym.FileID, sym.Language, sym.Kind, sym.Name, sym.Signature, sym.Doc, sym.LineStart, sym.LineEnd, sym.Receiver)
	return err
}

// DeleteSymbolsForFile removes all symbols associated with a file
func (db *DB) DeleteSymbolsForFile(fileID string) error {
	_, err := db.conn.Exec(`DELETE FROM symbols WHERE file_id = ?`, fileID)
	return err
}

// SearchSymbols searches for symbols by name (partial match)
func (db *DB) SearchSymbols(query string, kind string) ([]*Symbol, error) {
	var rows *sql.Rows
	var err error

	if kind != "" {
		rows, err = db.conn.Query(`
			SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
			FROM symbols WHERE name LIKE ? AND kind = ?
			ORDER BY name
		`, "%"+query+"%", kind)
	} else {
		rows, err = db.conn.Query(`
			SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
			FROM symbols WHERE name LIKE ?
			ORDER BY name
		`, "%"+query+"%")
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// GetSymbol retrieves a symbol by ID
func (db *DB) GetSymbol(id string) (*Symbol, error) {
	row := db.conn.QueryRow(`
		SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
		FROM symbols WHERE id = ?
	`, id)

	var s Symbol
	err := row.Scan(&s.ID, &s.FileID, &s.Language, &s.Kind, &s.Name, &s.Signature, &s.Doc, &s.LineStart, &s.LineEnd, &s.Receiver)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetSymbolsForFile retrieves all symbols for a given file
func (db *DB) GetSymbolsForFile(fileID string) ([]*Symbol, error) {
	rows, err := db.conn.Query(`
		SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
		FROM symbols WHERE file_id = ?
		ORDER BY line_start
	`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSymbols(rows)
}

// Stats returns database statistics
func (db *DB) Stats() (fileCount, symbolCount int64, err error) {
	err = db.conn.QueryRow(`SELECT COUNT(*) FROM files`).Scan(&fileCount)
	if err != nil {
		return 0, 0, fmt.Errorf("counting files: %w", err)
	}

	err = db.conn.QueryRow(`SELECT COUNT(*) FROM symbols`).Scan(&symbolCount)
	if err != nil {
		return 0, 0, fmt.Errorf("counting symbols: %w", err)
	}

	return fileCount, symbolCount, nil
}

// scanSymbols helper to scan multiple symbol rows
func scanSymbols(rows *sql.Rows) ([]*Symbol, error) {
	var symbols []*Symbol
	for rows.Next() {
		var s Symbol
		err := rows.Scan(&s.ID, &s.FileID, &s.Language, &s.Kind, &s.Name, &s.Signature, &s.Doc, &s.LineStart, &s.LineEnd, &s.Receiver)
		if err != nil {
			return nil, err
		}
		symbols = append(symbols, &s)
	}
	return symbols, rows.Err()
}

// SaveReference inserts a reference record
func (db *DB) SaveReference(ref *Reference) error {
	_, err := db.conn.Exec(`
		INSERT INTO symbol_refs (source_id, target_name, target_id, kind, line)
		VALUES (?, ?, ?, ?, ?)
	`, ref.SourceID, ref.TargetName, ref.TargetID, ref.Kind, ref.Line)
	return err
}

// DeleteReferencesForFile removes all references where source symbols belong to a file
func (db *DB) DeleteReferencesForFile(fileID string) error {
	_, err := db.conn.Exec(`
		DELETE FROM symbol_refs
		WHERE source_id IN (SELECT id FROM symbols WHERE file_id = ?)
	`, fileID)
	return err
}

// GetReferencesFrom retrieves all outgoing references from a symbol
func (db *DB) GetReferencesFrom(sourceID string) ([]*Reference, error) {
	rows, err := db.conn.Query(`
		SELECT id, source_id, target_name, target_id, kind, line
		FROM symbol_refs WHERE source_id = ?
		ORDER BY line
	`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReferences(rows)
}

// GetReferencesTo retrieves all incoming references to a symbol (by target_id)
func (db *DB) GetReferencesTo(targetID string) ([]*Reference, error) {
	rows, err := db.conn.Query(`
		SELECT id, source_id, target_name, target_id, kind, line
		FROM symbol_refs WHERE target_id = ?
		ORDER BY line
	`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReferences(rows)
}

// GetReferencesByName retrieves references by target name (for unresolved refs)
func (db *DB) GetReferencesByName(targetName string) ([]*Reference, error) {
	rows, err := db.conn.Query(`
		SELECT id, source_id, target_name, target_id, kind, line
		FROM symbol_refs WHERE target_name = ?
		ORDER BY line
	`, targetName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReferences(rows)
}

// FindSymbolByName finds symbols by name (heuristic resolution)
func (db *DB) FindSymbolByName(name string, fileID string) ([]*Symbol, error) {
	// First try: exact match in same file
	rows, err := db.conn.Query(`
		SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
		FROM symbols WHERE name = ? AND file_id = ?
		ORDER BY kind
	`, name, fileID)
	if err != nil {
		return nil, err
	}
	symbols, err := scanSymbols(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(symbols) > 0 {
		return symbols, nil
	}

	// Second try: exact match anywhere
	rows, err = db.conn.Query(`
		SELECT id, file_id, language, kind, name, signature, doc, line_start, line_end, receiver
		FROM symbols WHERE name = ?
		ORDER BY kind
	`, name)
	if err != nil {
		return nil, err
	}
	return scanSymbols(rows)
}

// UpdateReferenceTarget updates the target_id of a reference
func (db *DB) UpdateReferenceTarget(refID int64, targetID string) error {
	_, err := db.conn.Exec(`
		UPDATE symbol_refs SET target_id = ? WHERE id = ?
	`, targetID, refID)
	return err
}

// GetFileLanguageStats returns language statistics for map command
func (db *DB) GetFileLanguageStats() (map[string]int64, error) {
	rows, err := db.conn.Query(`
		SELECT language, COUNT(*) FROM files WHERE language != '' GROUP BY language
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]int64)
	for rows.Next() {
		var lang string
		var count int64
		if err := rows.Scan(&lang, &count); err != nil {
			return nil, err
		}
		stats[lang] = count
	}
	return stats, rows.Err()
}

// GetDirectoryStats returns file and symbol counts per directory
func (db *DB) GetDirectoryStats() (map[string]struct{ Files, Symbols int64 }, error) {
	rows, err := db.conn.Query(`
		SELECT
			CASE
				INSTR(id, '/')
				WHEN 0 THEN '.'
				ELSE SUBSTR(id, 1, INSTR(id, '/') - 1)
			END as dir,
			COUNT(*) as files
		FROM files
		GROUP BY dir
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make(map[string]struct{ Files, Symbols int64 })
	for rows.Next() {
		var dir string
		var files int64
		if err := rows.Scan(&dir, &files); err != nil {
			return nil, err
		}
		stats[dir] = struct{ Files, Symbols int64 }{Files: files, Symbols: 0}
	}
	rows.Close()

	// Get symbol counts per directory
	rows, err = db.conn.Query(`
		SELECT
			CASE
				INSTR(s.file_id, '/')
				WHEN 0 THEN '.'
				ELSE SUBSTR(s.file_id, 1, INSTR(s.file_id, '/') - 1)
			END as dir,
			COUNT(*) as symbols
		FROM symbols s
		GROUP BY dir
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var dir string
		var symbols int64
		if err := rows.Scan(&dir, &symbols); err != nil {
			return nil, err
		}
		if entry, ok := stats[dir]; ok {
			entry.Symbols = symbols
			stats[dir] = entry
		}
	}
	return stats, rows.Err()
}

// scanReferences helper to scan multiple reference rows
func scanReferences(rows *sql.Rows) ([]*Reference, error) {
	var refs []*Reference
	for rows.Next() {
		var r Reference
		var targetID sql.NullString
		err := rows.Scan(&r.ID, &r.SourceID, &r.TargetName, &targetID, &r.Kind, &r.Line)
		if err != nil {
			return nil, err
		}
		if targetID.Valid {
			r.TargetID = &targetID.String
		}
		refs = append(refs, &r)
	}
	return refs, rows.Err()
}

// BeginTx starts a new transaction
func (db *DB) BeginTx() (*sql.Tx, error) {
	return db.conn.Begin()
}

// Conn returns the raw database connection (for advanced use)
func (db *DB) Conn() *sql.DB {
	return db.conn
}
