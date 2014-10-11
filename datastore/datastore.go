package datastore

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
	"github.com/petert82/go-translation-api/trans"
	"github.com/petert82/go-translation-api/xliff"
	"path/filepath"
	"time"
)

type DataStore struct {
	db          *sqlx.DB
	domainCache map[string]int64
	stringCache map[StringKey]int64
}

type StringKey struct {
	DomainId int64
	Name     string
}

func New(db *sqlx.DB) (ds *DataStore, err error) {
	ds = &DataStore{db: db, domainCache: make(map[string]int64), stringCache: make(map[StringKey]int64)}

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		return ds, err
	}
	_, err = db.Exec("PRAGMA journal_mode = WAL")
	if err != nil {
		return ds, err
	}
	_, err = db.Exec("PRAGMA synchronous = NORMAL")
	if err != nil {
		return ds, err
	}
	return ds, nil
}

func (ds *DataStore) getLanguage(code string) (l trans.Language, err error) {
	err = ds.db.Get(&l, "SELECT id, name, code FROM language WHERE code=?", code)
	if err != nil {
		if err == sql.ErrNoRows {
			return l, errors.New(fmt.Sprintf("Language '%v' does not exist in database", code))
		}

		return l, err
	}

	return l, nil
}

func (ds *DataStore) getDomainId(name string) (id int64, err error) {
	if id, ok := ds.domainCache[name]; ok {
		return id, nil
	}

	row := ds.db.QueryRow("SELECT id FROM domain WHERE name=? ", name)
	err = row.Scan(&id)
	if err != nil {
		return 0, err
	}
	ds.domainCache[name] = id

	return id, nil
}

func (ds *DataStore) createDomain(name string) (id int64, err error) {
	result, err := ds.db.Exec("INSERT INTO domain (name) VALUES (?)", name)
	if err != nil {
		return 0, nil
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (ds *DataStore) createOrGetDomain(name string) (id int64, err error) {
	id, err = ds.getDomainId(name)

	if err == sql.ErrNoRows {
		return ds.createDomain(name)
	}

	return id, err
}

var getTime, insertTime, updateTime, stringTime time.Duration

func (ds *DataStore) getTranslationId(t *trans.Translation, domainId int64) (id int, err error) {
	start := time.Now()
	row := ds.db.QueryRow("SELECT translation.id FROM string INNER JOIN translation ON string.id = translation.string_id WHERE name=? AND language_id=? AND domain_id=?", t.Name, t.Language.Id, domainId)
	err = row.Scan(&id)
	if err != nil {
		return 0, err
	}
	getTime += time.Since(start)
	return id, nil
}

func (ds *DataStore) getStringId(name string, domainId int64) (id int64, err error) {
	row := ds.db.QueryRow("SELECT id FROM string WHERE name = ? AND domain_id = ?", name, domainId)
	err = row.Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (ds *DataStore) createString(name string, domainId int64) (id int64, err error) {
	result, err := ds.db.Exec(`INSERT INTO string (name, domain_id) VALUES (?, ?)`, name, domainId)
	if err != nil {
		return 0, err
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (ds *DataStore) createOrGetString(name string, domainId int64) (id int64, err error) {
	start := time.Now()
	id, err = ds.getStringId(name, domainId)

	if err == sql.ErrNoRows {
		id, err = ds.createString(name, domainId)
	}

	stringTime += time.Since(start)
	return id, err
}

func (ds *DataStore) insertTranslation(t *trans.Translation, stringId int64, domainId int64) (err error) {
	start := time.Now()
	_, err = ds.db.Exec(`INSERT INTO translation (language_id, content, string_id) VALUES (?, ?, ?)`, t.Language.Id, t.Content, stringId)
	insertTime += time.Since(start)
	fmt.Printf("Inserting: %v\n", t.Name)
	return err
}

func (ds *DataStore) updateTranslation(t *trans.Translation, stringId int64, domainId int64) (err error) {
	start := time.Now()
	_, err = ds.db.Exec(`UPDATE translation SET language_id=?, content=?, string_id=? WHERE id=?`, t.Language.Id, t.Content, stringId, t.Id)
	updateTime += time.Since(start)
	return err
}

func (ds *DataStore) ImportDomain(d trans.Domain) (err error) {
	l, err := ds.getLanguage(d.Language())
	if err != nil {
		return err
	}

	domId, err := ds.createOrGetDomain(d.Name())
	if err != nil {
		return err
	}

	for _, t := range d.Translations() {
		t.Language = &l

		stringId, ok := ds.stringCache[StringKey{DomainId: domId, Name: t.Name}]
		if !ok {
			stringId, err = ds.createOrGetString(t.Name, domId)
			if err != nil {
				return err
			}
			ds.stringCache[StringKey{DomainId: domId, Name: t.Name}] = stringId
		}

		if t.Id, err = ds.getTranslationId(&t, domId); err != nil {
			if err == sql.ErrNoRows {
				err = ds.insertTranslation(&t, stringId, domId)
			}
		} else {
			err = ds.updateTranslation(&t, stringId, domId)
		}

		if err != nil {
			return err
		}
	}

	return nil
}

func (ds *DataStore) ImportDir(dir string, notify chan string) (count int, err error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.xliff"))
	if err != nil {
		return 0, nil
	}

	var xliffTime time.Duration
	for i, file := range files {
		start := time.Now()
		xliff, err := xliff.NewFromFile(file)
		if err != nil {
			return i, err
		}
		xliffTime += time.Since(start)

		err = ds.ImportDomain(&xliff.File.XliffDomain)
		if err != nil {
			return i, err
		}

		notify <- filepath.Base(file)
	}
	fmt.Printf("Parsing files took: %v\n", xliffTime)
	fmt.Printf("Creating strings took: %v\n", stringTime)
	fmt.Printf("Getting translation IDs took: %v\n", getTime)
	fmt.Printf("Translation INSERT queries took: %v\n", insertTime)
	fmt.Printf("Trabslation UPDATE queries took: %v\n", updateTime)

	return len(files), nil
}
