package cache

import (
	"bytes"
	"compress/gzip"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"
	"sync"
	"time"
)

type binarySerializer interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type textSerializer interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

type diskCacheMemRecord[V any] struct {
	createdAt      time.Time
	lastReadAtLock *sync.Mutex
	lastReadAt     *time.Time
	value          V
}

type DiskCache[K comparable, V any] struct {
	basePath   string
	rwm        sync.RWMutex
	m          map[K]diskCacheMemRecord[V]
	maxSize    int
	size       int
	zipEnabled bool
}

func NewDiskCache[K comparable, V any](path string, maxSize int, zipEnabled bool) (*DiskCache[K, V], error) {
	if maxSize < 0 {
		maxSize = 0
	}

	fi, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(path, fs.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to make cache directory: %w", err)
		}
	} else if !fi.IsDir() {
		return nil, errors.New("existing path is not a directory")
	}

	return &DiskCache[K, V]{
		basePath:   path,
		m:          make(map[K]diskCacheMemRecord[V], maxSize),
		maxSize:    maxSize,
		zipEnabled: zipEnabled,
	}, nil
}

func (c *DiskCache[K, V]) Get(k K) (V, bool, error) {
	var result V

	c.rwm.RLock()
	defer c.rwm.RUnlock()

	v, ok := c.m[k]
	if ok {
		func(m *sync.Mutex, lr *time.Time) {
			m.Lock()
			defer m.Unlock()

			*lr = time.Now()
		}(v.lastReadAtLock, v.lastReadAt)

		result = v.value
		return result, true, nil
	}

	// check if on disk
	{
		fp, err := c.filePath(k)
		if err != nil {
			return result, false, err
		}

		ok, err := fileExistsOnDisk(fp)
		if err != nil {
			return result, false, err
		}

		if ok {

			v, err := c.fileToValue(fp)
			if err != nil {
				return result, false, fmt.Errorf("failed to deserialize file contents: %w", err)
			}

			// set back in memory cache
			if c.maxSize > 0 {
				defer func() {
					c.rwm.Lock()
					defer c.rwm.Unlock()

					c.prepForNewRecord()

					now := time.Now()
					nowCopy := now
					c.m[k] = diskCacheMemRecord[V]{
						createdAt:      now,
						lastReadAtLock: &sync.Mutex{},
						lastReadAt:     &nowCopy,
						value:          v,
					}
				}()
			}

			result = v
			return result, true, nil
		}
	}

	return result, false, nil
}

func (c *DiskCache[K, V]) Set(k K, v V) error {
	c.rwm.Lock()
	defer c.rwm.Unlock()

	oldV, ok := c.m[k]

	if c.maxSize > 0 {

		var createdAt time.Time
		var lastReadAt *time.Time
		var lastReadAtLock *sync.Mutex
		if ok {

			createdAt = oldV.createdAt
			lastReadAtLock = oldV.lastReadAtLock
			lastReadAt = oldV.lastReadAt
		} else {
			c.prepForNewRecord()

			createdAt = time.Now()
			lastReadAtLock = &sync.Mutex{}
			lastReadAt = &time.Time{}
		}

		c.m[k] = diskCacheMemRecord[V]{
			createdAt:      createdAt,
			lastReadAtLock: lastReadAtLock,
			lastReadAt:     lastReadAt,
			value:          v,
		}
	}

	return c.saveToDisk(k, v)
}

func (c *DiskCache[K, V]) saveToDisk(k K, v V) error {
	fk, err := c.filePath(k)
	if err != nil {
		return fmt.Errorf("failed to encode key: %w", err)
	}

	valBytes, err := c.valueToBytes(v)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	return os.WriteFile(path.Join(c.basePath, fk), valBytes, 0600)
}

func (c *DiskCache[K, V]) filePath(k K) (string, error) {
	abstractK := any(k)

	var fileKey string
	switch abstractV := abstractK.(type) {
	case []byte:
		fileKey = string(abstractV)
	case []rune:
		fileKey = string(abstractV)
	case string:
		fileKey = string(abstractV)
	default:
		if v, ok := abstractK.(interface{ MarshalText() ([]byte, error) }); ok {
			val, err := v.MarshalText()
			if err != nil {
				return "", fmt.Errorf("failed to MarshalText: %w", err)
			}

			fileKey = string(val)
		} else if v, ok := abstractK.(interface{ MarshalBinary() ([]byte, error) }); ok {
			val, err := v.MarshalBinary()
			if err != nil {
				return "", fmt.Errorf("failed to MarshalBinary: %w", err)
			}

			fileKey = string(val)
		} else if v, ok := abstractK.(interface{ MarshalJSON() ([]byte, error) }); ok {
			val, err := v.MarshalJSON()
			if err != nil {
				return "", fmt.Errorf("failed to MarshalJSON: %w", err)
			}

			fileKey = strings.TrimRight(string(val), "\n")
		} else {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(k); err != nil {
				return "", fmt.Errorf("failed to json.Encode: %w", err)
			}

			fileKey = strings.TrimRight(buf.String(), "\n")
		}
	}

	return base64.RawURLEncoding.EncodeToString([]byte(fileKey)), nil
}

func (c *DiskCache[K, V]) valueToBytes(v V) ([]byte, error) {
	av := any(v)

	if vm, ok := av.(binarySerializer); ok {
		return vm.MarshalBinary()
	}

	if vm, ok := av.(textSerializer); ok {
		b, err := vm.MarshalText()
		if err != nil {
			return nil, err
		}

		if !c.zipEnabled {
			return b, nil
		}

		return zip(b)
	}

	switch tv := av.(type) {
	case []byte:
		if !c.zipEnabled {
			return tv, nil
		}
		return zip(tv)
	case []rune:
		if !c.zipEnabled {
			return []byte(string(tv)), nil
		}
		return zip([]byte(string(tv)))
	case string:
		if !c.zipEnabled {
			return []byte(tv), nil
		}
		return zip([]byte(tv))
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	if !c.zipEnabled {
		return buf.Bytes(), nil
	}

	return zip(buf.Bytes())
}

func (c *DiskCache[K, V]) bytesToValue(b []byte) (V, error) {
	var result V
	var buf V

	var av any
	vIsPointerType := isPointerType(buf)
	if !vIsPointerType {
		av = any(&buf)
	} else {
		av = any(buf)
	}

	if v, ok := av.(binarySerializer); ok {
		if err := v.UnmarshalBinary(b); err != nil {
			return result, err
		}

		result = buf
		return result, nil
	}

	if v, ok := av.(textSerializer); ok {
		if c.zipEnabled {
			if v, err := unzip(b); err != nil {
				return result, err
			} else {
				b = v
			}
		}

		if err := v.UnmarshalText(b); err != nil {
			return result, err
		}

		result = buf
		return result, nil
	}

	if c.zipEnabled {
		if v, err := unzip(b); err != nil {
			return result, err
		} else {
			b = v
		}
	}

	if !vIsPointerType {
		switch x := av.(type) {
		case *[]byte:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf(b))
			return buf, nil
		case *[]rune:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf([]rune(string(b))))
			return buf, nil
		case *string:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf(string(b)))
			return buf, nil
		}
	}

	if err := json.Unmarshal(b, &buf); err != nil {
		return result, err
	}

	result = buf
	return result, nil
}

func (c *DiskCache[K, V]) fileToValue(path string) (V, error) {
	var result V

	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer f.Close()

	b, err := io.ReadAll(f)
	if err != nil {
		return result, err
	}

	v, err := c.bytesToValue(b)
	if err != nil {
		return result, err
	}

	result = v
	return result, nil
}

func (c *DiskCache[K, V]) Delete(k K) error {
	c.rwm.RLock()
	cleanup := c.rwm.RUnlock
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

	fp, err := c.filePath(k)
	if err != nil {
		return err
	}

	var fsOK bool
	_, ramOK := c.m[k]
	if !ramOK {
		fsOK, err = fileExistsOnDisk(fp)
		if err != nil {
			return err
		}
	}

	if !ramOK && !fsOK {
		return nil
	}

	if f := cleanup; f != nil {
		cleanup = nil
		f()
	}

	c.rwm.Lock()
	cleanup = c.rwm.Unlock

	_, ramOK = c.m[k]
	fsOK, err = fileExistsOnDisk(fp)
	if err != nil {
		return err
	}

	if ramOK {
		c.size -= 1
		delete(c.m, k)
	}

	if fsOK {
		if err := os.Remove(fp); err != nil {
			return err
		}
	}

	return nil
}

func (c *DiskCache[K, V]) prepForNewRecord() {
	if c.size < c.maxSize {
		c.size += 1
		return
	}

	// otherwise we're at the max value and need to remove an element

	var keyToRemove K
	var valueToRemove diskCacheMemRecord[V]
	var valueToRemoveLastReadAtIsZero bool
	for k, v := range c.m {
		keyToRemove = k
		valueToRemove = v
		valueToRemoveLastReadAtIsZero = v.lastReadAt.IsZero()
		break
	}

	// TODO: refactor from O(n) to a more constant alg
	for k, v := range c.m {

		var swap bool
		if valueToRemoveLastReadAtIsZero {
			swap = (v.lastReadAt.IsZero() && v.createdAt.Before(valueToRemove.createdAt))
		} else if v.lastReadAt.IsZero() {
			swap = true
		} else if valueToRemove.lastReadAt.After(*v.lastReadAt) {
			swap = true
		} else if !valueToRemove.lastReadAt.Before(*v.lastReadAt) && v.createdAt.Before(valueToRemove.createdAt) {
			swap = true
		}

		if swap {
			keyToRemove = k
			valueToRemove = v
			valueToRemoveLastReadAtIsZero = v.lastReadAt.IsZero()
		}
	}

	delete(c.m, keyToRemove)

	// leave record on disk
}

func fileExistsOnDisk(path string) (bool, error) {

	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if fi.IsDir() {
		return false, errors.New("is a dir and not a file: " + path)
	}

	return true, nil
}

func zip(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)

	if _, err := w.Write(b); err != nil {
		return nil, err
	}

	if err := w.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func unzip(b []byte) ([]byte, error) {

	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}

	res, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func isPointerType(x any) bool {
	return (reflect.ValueOf(x).Type().Kind() == reflect.Pointer)
}
