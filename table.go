package genji

import (
	"time"

	"github.com/asdine/genji/index"

	"github.com/asdine/genji/engine"
	"github.com/asdine/genji/field"
	"github.com/asdine/genji/record"
	"github.com/asdine/genji/table"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
)

// A Table represents a collection of records.
type Table struct {
	tx    *Tx
	store engine.Store
	name  string
}

// Iterate goes through all the records of the table and calls the given function by passing each one of them.
// If the given function returns an error, the iteration stops.
func (t Table) Iterate(fn func(recordID []byte, r record.Record) error) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID, v []byte) error {
		return fn(recordID, record.EncodedRecord(v))
	})
}

// Record returns one record by recordID.
func (t Table) Record(recordID []byte) (record.Record, error) {
	v, err := t.store.Get(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return nil, table.ErrRecordNotFound
		}
		return nil, errors.Wrapf(err, "failed to fetch record %q", recordID)
	}

	return record.EncodedRecord(v), err
}

// Insert the record into the table.
// If the record implements the table.Pker interface, it will be used to generate a recordID,
// otherwise it will be generated automatically. Note that there are no ordering guarantees
// regarding the recordID generated by default.
func (t Table) Insert(r record.Record) ([]byte, error) {
	v, err := record.Encode(r)
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode record")
	}

	var recordID []byte
	if pker, ok := r.(table.Pker); ok {
		recordID, err = pker.Pk()
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate recordID from Pk method")
		}
	} else {
		id, err := ulid.New(ulid.Timestamp(time.Now()), entropy)
		if err == nil {
			recordID, err = id.MarshalText()
		}
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate recordID")
		}
	}

	_, err = t.store.Get(recordID)
	if err == nil {
		return nil, table.ErrDuplicate
	}

	err = t.store.Put(recordID, v)
	if err != nil {
		return nil, err
	}

	indexes, err := t.tx.Indexes(t.name)
	if err != nil {
		return nil, err
	}

	for fieldName, idx := range indexes {
		f, err := r.Field(fieldName)
		if err != nil {
			return nil, err
		}

		err = idx.Set(f.Data, recordID)
		if err != nil {
			if err == index.ErrDuplicate {
				return nil, table.ErrDuplicate
			}

			return nil, err
		}
	}

	return recordID, nil
}

// Delete a record by recordID.
// Indexes are automatically updated.
func (t Table) Delete(recordID []byte) error {
	err := t.store.Delete(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return table.ErrRecordNotFound
		}
		return err
	}

	indexes, err := t.tx.Indexes(t.name)
	if err != nil {
		return err
	}

	for _, idx := range indexes {
		err = idx.Delete(recordID)
		if err != nil {
			return err
		}
	}

	return nil
}

type pkWrapper struct {
	record.Record
	pk []byte
}

func (p pkWrapper) Pk() ([]byte, error) {
	return p.pk, nil
}

// Replace a record by recordID.
// An error is returned if the recordID doesn't exist.
// Indexes are automatically updated.
func (t Table) Replace(recordID []byte, r record.Record) error {
	err := t.Delete(recordID)
	if err != nil {
		if err == engine.ErrKeyNotFound {
			return table.ErrRecordNotFound
		}
		return err
	}

	_, err = t.Insert(pkWrapper{Record: r, pk: recordID})
	return err
}

// Truncate deletes all the records from the table.
func (t Table) Truncate() error {
	return t.store.Truncate()
}

// AddField changes the table structure by adding a field to all the records.
// If the field data is empty, it is filled with the zero value of the field type.
// If a record already has the field, no change is performed on that record.
func (t Table) AddField(f field.Field) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		if _, err = fb.Field(f.Name); err == nil {
			// if the field already exists, skip
			return nil
		}

		if f.Data == nil {
			f.Data = field.ZeroValue(f.Type).Data
		}
		fb.Add(f)

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

// DeleteField changes the table structure by deleting a field from all the records.
func (t Table) DeleteField(name string) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID []byte, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		err = fb.Delete(name)
		if err != nil {
			// if the field doesn't exist, skip
			return nil
		}

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

// RenameField changes the table structure by renaming the selected field on all the records.
func (t Table) RenameField(oldName, newName string) error {
	return t.store.AscendGreaterOrEqual(nil, func(recordID []byte, v []byte) error {
		var fb record.FieldBuffer
		err := fb.ScanRecord(record.EncodedRecord(v))
		if err != nil {
			return err
		}

		f, err := fb.Field(oldName)
		if err != nil {
			// if the field doesn't exist, skip
			return nil
		}

		f.Name = newName
		fb.Replace(oldName, f)

		v, err = record.Encode(&fb)
		if err != nil {
			return err
		}

		return t.store.Put(recordID, v)
	})
}

// ReIndex drops the selected index, creates a new one and runs over all the records
// to fill the newly created index.
func (t Table) ReIndex(fieldName string) error {
	err := t.tx.DropIndex(t.name, fieldName)
	if err != nil {
		return err
	}

	indexName := buildIndexName(t.name, fieldName)

	opts, err := readIndexOptions(t.tx, indexName)
	if err != nil {
		return err
	}

	err = t.tx.CreateIndex(t.name, fieldName, index.Options{Unique: opts.Unique})
	if err != nil {
		return err
	}

	idx, err := t.tx.Index(t.name, fieldName)
	if err != nil {
		return err
	}

	return t.Iterate(func(recordID []byte, r record.Record) error {
		f, err := r.Field(fieldName)
		if err != nil {
			return err
		}

		return idx.Set(f.Data, recordID)
	})
}

// SelectTable returns the current table. Implements the query.TableSelector interface.
func (t Table) SelectTable(*Tx) (*Table, error) {
	return &t, nil
}

// TableName returns the name of the table.
func (t Table) TableName() string {
	return t.name
}

// A TableNamer is a type that returns the name of a table.
type TableNamer interface {
	TableName() string
}

type indexer interface {
	Indexes() map[string]index.Options
}

// InitTable ensures the table exists. If tn implements this interface
//   type indexer interface {
//	  Indexes() map[string]index.Options
//   }
// it ensures all these indexes exist and creates them if not, but it won't re-index the table.
func InitTable(tx *Tx, tn TableNamer) error {
	name := tn.TableName()

	err := tx.CreateTableIfNotExists(name)
	if err != nil {
		return err
	}

	idxr, ok := tn.(indexer)
	if ok {
		for fieldName, idxOpts := range idxr.Indexes() {
			err = tx.CreateIndexIfNotExists(name, fieldName, idxOpts)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
