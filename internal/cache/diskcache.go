package cache

import (
	"bytes"
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

// TODO: key and value removal could be extended to call some finalizer implementation and signal if it is
// a ram-record delete, a disk-record delete, or both

type Marshaler[V any] interface {
	Marshal(V) ([]byte, error)
}

type Transcoder[V any] interface {
	Marshaler[V]
	Unmarshal([]byte) (V, error)
}

type defaultKeyMarshaler[K comparable] struct{}

func (km *defaultKeyMarshaler[K]) Marshal(k K) ([]byte, error) {
	ak := any(k)

	var fileKey string
	switch x := ak.(type) {
	case []byte:
		fileKey = string(x)
	case []rune:
		fileKey = string(x)
	case string:
		fileKey = string(x)
	default:
		if v, ok := ak.(encoding.TextMarshaler); ok {
			val, err := v.MarshalText()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalText: %w", err)
			}

			fileKey = string(val)
		} else if v, ok := ak.(encoding.BinaryMarshaler); ok {
			val, err := v.MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalBinary: %w", err)
			}

			fileKey = string(val)
		} else if v, ok := ak.(json.Marshaler); ok {
			val, err := v.MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalJSON: %w", err)
			}

			fileKey = strings.TrimRight(string(val), "\n")
		} else {
			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)
			if err := enc.Encode(k); err != nil {
				return nil, fmt.Errorf("failed to json.Encode: %w", err)
			}

			fileKey = strings.TrimRight(buf.String(), "\n")
		}
	}

	return []byte(fileKey), nil
}

func newDefaultKeyMarshaler[K comparable]() Marshaler[K] {
	return &defaultKeyMarshaler[K]{}
}

type defaultValueTranscoder[V any] struct {
	valIsPointerType bool
}

func (vt *defaultValueTranscoder[V]) Marshal(v V) ([]byte, error) {
	av := any(v)

	if vm, ok := av.(binaryTranscoder); ok {
		return vm.MarshalBinary()
	}

	if vm, ok := av.(textTranscoder); ok {
		b, err := vm.MarshalText()
		if err != nil {
			return nil, err
		}

		return b, nil
	}

	switch x := av.(type) {
	case []byte:
		return x, nil
	case []rune:
		return []byte(string(x)), nil
	case string:
		return []byte(x), nil
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)

	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (vt *defaultValueTranscoder[V]) Unmarshal(b []byte) (V, error) {
	var result V
	var buf V

	var av any
	if !vt.valIsPointerType {
		av = any(&buf)
	} else {
		av = any(buf)
	}

	if v, ok := av.(binaryTranscoder); ok {
		if err := v.UnmarshalBinary(b); err != nil {
			return result, err
		}

		result = buf
		return result, nil
	}

	if v, ok := av.(textTranscoder); ok {

		if err := v.UnmarshalText(b); err != nil {
			return result, err
		}

		result = buf
		return result, nil
	}

	if !vt.valIsPointerType {
		switch x := av.(type) {
		case *[]byte:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf(b))
			result = buf
			return result, nil
		case *[]rune:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf([]rune(string(b))))
			result = buf
			return result, nil
		case *string:
			reflect.ValueOf(x).Elem().Set(reflect.ValueOf(string(b)))
			result = buf
			return result, nil
		}
	}

	if err := json.Unmarshal(b, &buf); err != nil {
		return result, err
	}

	result = buf
	return result, nil
}

func newDefaultValueTranscoder[V any]() Transcoder[V] {
	var v V
	return &defaultValueTranscoder[V]{
		valIsPointerType: (reflect.ValueOf(v).Type().Kind() == reflect.Pointer),
	}
}

type binaryTranscoder interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type textTranscoder interface {
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
	basePath      string
	rwm           sync.RWMutex
	m             map[K]diskCacheMemRecord[V]
	keyMarshaler  Marshaler[K]
	valTranscoder Transcoder[V]
	maxSize       int
	size          int
}

type diskCacheOptions[K comparable, V any] struct {
	valTranscoder                     Transcoder[V]
	keyMarshaler                      Marshaler[K]
	valTranscoderSet, keyMarshalerSet bool
}

type DiskCacheOption[K comparable, V any] func(*diskCacheOptions[K, V])

func DiskCacheValueTranscoder[K comparable, V any](t Transcoder[V]) DiskCacheOption[K, V] {
	return func(opt *diskCacheOptions[K, V]) {
		opt.valTranscoder = t
		opt.valTranscoderSet = true
	}
}

func DiskCacheKeyMarshaler[K comparable, V any](m Marshaler[K]) DiskCacheOption[K, V] {
	return func(opt *diskCacheOptions[K, V]) {
		opt.keyMarshaler = m
		opt.keyMarshalerSet = true
	}
}

func NewDiskCache[K comparable, V any](path string, maxSize int, options ...DiskCacheOption[K, V]) (*DiskCache[K, V], error) {
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

	cfg := diskCacheOptions[K, V]{}

	for _, op := range options {
		op(&cfg)
	}

	if !cfg.keyMarshalerSet {
		cfg.keyMarshaler = newDefaultKeyMarshaler[K]()
	}

	if !cfg.valTranscoderSet {
		cfg.valTranscoder = newDefaultValueTranscoder[V]()
	}

	return &DiskCache[K, V]{
		basePath:      path,
		m:             make(map[K]diskCacheMemRecord[V], maxSize),
		keyMarshaler:  cfg.keyMarshaler,
		valTranscoder: cfg.valTranscoder,
		maxSize:       maxSize,
	}, nil
}

func (c *DiskCache[K, V]) Get(k K) (V, bool, error) {
	var result V

	c.rwm.RLock()
	cleanup := c.rwm.RUnlock
	defer func() {
		if f := cleanup; f != nil {
			cleanup = nil
			f()
		}
	}()

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
		fp, err := c.keyFilePath(k)
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

			result = v

			// set back in memory cache
			if c.maxSize > 0 {

				if f := cleanup; f != nil {
					cleanup = nil
					f()
				}
				c.rwm.Lock()
				cleanup = c.rwm.Unlock

				c.prepForNewRecord()

				now := time.Now()
				nowCopy := now
				c.m[k] = diskCacheMemRecord[V]{
					createdAt:      now,
					lastReadAtLock: &sync.Mutex{},
					lastReadAt:     &nowCopy,
					value:          result,
				}
			}

			return result, true, nil
		}
	}

	return result, false, nil
}

func (c *DiskCache[K, V]) Set(k K, v V) error {
	c.rwm.Lock()
	defer c.rwm.Unlock()

	if c.maxSize > 0 {
		oldV, ok := c.m[k]

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
	fp, err := c.keyFilePath(k)
	if err != nil {
		return fmt.Errorf("failed to encode key: %w", err)
	}

	b, err := c.valTranscoder.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	return os.WriteFile(fp, b, 0600)
}

func (c *DiskCache[K, V]) keyFilePath(k K) (string, error) {

	b, err := c.keyMarshaler.Marshal(k)
	if err != nil {
		return "", err
	}

	fileName := base64.RawURLEncoding.EncodeToString(b)

	return path.Join(c.basePath, fileName), nil
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

	v, err := c.valTranscoder.Unmarshal(b)
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

	fp, err := c.keyFilePath(k)
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
