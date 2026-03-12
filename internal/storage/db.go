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
`

// DB wraps the SQLite database connection
type DB struct {
	conn *sql.DB
	path string
}

// File represents a source file in the database
type File struct {
	ID             string
	AbsolutePath   string
	Language       string
	SizeBytes      int64
	Mtime          int64
	IndexedAt      int64
	GitSHA         string
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

// BeginTx starts a new transaction
func (db *DB) BeginTx() (*sql.Tx, error) {
	return db.conn.Begin()
}

// Conn returns the raw database connection (for advanced use)
func (db *DB) Conn() *sql.DB {
	return db.conn
}
