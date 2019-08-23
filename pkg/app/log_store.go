package app

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"go.etcd.io/bbolt"
)

// LogStore stores logs from apps, for later consumption from the hypervisor
type LogStore interface {
	// Write implements io.Writer
	Write(p []byte) (n int, err error)

	// Store saves given log in db
	Store(t time.Time, s string) error

	// LogSince returns the logs since given timestamp. For optimal performance,
	// the timestamp should exist in the store (you can get it from previous logs),
	// otherwise the DB will be sequentially iterated until finding entries older than given timestamp
	LogsSince(t time.Time) ([]string, error)
}

// NewLogStore returns a LogStore with path and app name of the given kind
func NewLogStore(path, appName, kind string) (LogStore, error) {
	switch kind {
	case "bbolt":
		return newBoltDB(path, appName)
	default:
		return nil, fmt.Errorf("no LogStore of type %s", kind)
	}
}

type boltDBappLogs struct {
	dbpath string
	bucket []byte
}

func newBoltDB(path, appName string) (LogStore, error) {
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := db.Close()
		if err != nil {
			panic(err)
		}
	}()

	b := []byte(appName)
	err = db.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(b); err != nil {
			return fmt.Errorf("failed to create bucket: %s", err)
		}

		return nil
	})
	if err != nil && !strings.Contains(err.Error(), bbolt.ErrBucketExists.Error()) {
		return nil, err
	}

	return &boltDBappLogs{path, b}, nil
}

// Write implements io.Writer
func (l *boltDBappLogs) Write(p []byte) (int, error) {
	db, err := bbolt.Open(l.dbpath, 0600, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		err := db.Close()
		if err != nil {
			panic(err)
		}
	}()

	// time in RFC3339Nano is between the bytes 1 and 36. This will change if other time layout is in use
	t := p[1:36]

	err = db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(l.bucket)
		return b.Put(t, p)
	})

	if err != nil {
		return 0, err
	}

	return len(p), nil
}

// Store implements LogStore
func (l *boltDBappLogs) Store(t time.Time, s string) error {
	db, err := bbolt.Open(l.dbpath, 0600, nil)
	if err != nil {
		return err
	}
	defer func() {
		err := db.Close()
		if err != nil {
			panic(err)
		}
	}()

	parsedTime := []byte(t.Format(time.RFC3339Nano))
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(l.bucket)
		return b.Put(parsedTime, []byte(s))
	})
}

// LogSince implements LogStore
func (l *boltDBappLogs) LogsSince(t time.Time) ([]string, error) {
	db, err := bbolt.Open(l.dbpath, 0600, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		err := db.Close()
		if err != nil {
			panic(err)
		}
	}()

	logs := make([]string, 0)

	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(l.bucket)
		parsedTime := []byte(t.Format(time.RFC3339Nano))
		c := b.Cursor()

		v := b.Get(parsedTime)
		if v == nil {
			return iterateFromBeginning(c, parsedTime, &logs)
		}
		c.Seek(parsedTime)
		return iterateFromKey(c, &logs)
	})

	return logs, err
}

func iterateFromKey(c *bbolt.Cursor, logs *[]string) error {
	for k, v := c.Next(); k != nil; k, v = c.Next() {
		*logs = append(*logs, string(v))
	}
	return nil
}

func iterateFromBeginning(c *bbolt.Cursor, parsedTime []byte, logs *[]string) error {
	for k, v := c.First(); k != nil; k, v = c.Next() {
		if bytes.Compare(k, parsedTime) < 0 {
			continue
		}
		*logs = append(*logs, string(v))
	}

	return nil
}

func bytesToTime(b []byte) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, string(b))
}