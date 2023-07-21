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

type binaryTranscoder interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type textTranscoder interface {
	encoding.TextMarshaler
	encoding.TextUnmarshaler
}

type jsonTranscoder interface {
	json.Marshaler
	json.Unmarshaler
}

type Marshaler[V any] interface {
	Marshal(V) ([]byte, error)
}

type marshaler[V any] struct {
	marshal func(V) ([]byte, error)
}

func (km *marshaler[V]) Marshal(v V) ([]byte, error) {
	return km.marshal(v)
}

// NewKeyMarshaler returns an instance of a Marshaler of a comparable type
func NewKeyMarshaler[K comparable](marshal func(K) ([]byte, error)) Marshaler[K] {
	return &marshaler[K]{marshal}
}

func newDefaultKeyMarshaler[K comparable]() Marshaler[K] {
	var ak any
	{
		var k K
		ak = any(k)
	}

	switch ak.(type) {
	case encoding.BinaryMarshaler:
		return NewKeyMarshaler(func(k K) ([]byte, error) {
			val, err := any(k).(encoding.BinaryMarshaler).MarshalBinary()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalBinary: %w", err)
			}

			return val, nil
		})
	case encoding.TextMarshaler:
		return NewKeyMarshaler(func(k K) ([]byte, error) {
			val, err := any(k).(encoding.TextMarshaler).MarshalText()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalText: %w", err)
			}

			return val, nil
		})
	case json.Marshaler:
		return NewKeyMarshaler(func(k K) ([]byte, error) {
			val, err := any(k).(json.Marshaler).MarshalJSON()
			if err != nil {
				return nil, fmt.Errorf("failed to MarshalJSON: %w", err)
			}

			return []byte(strings.TrimRight(string(val), "\n")), nil
		})
	case string:
		return NewKeyMarshaler(func(k K) ([]byte, error) {
			v, ok := any(k).(string)
			if !ok {
				panic(errors.New("unreachable"))
			}

			return []byte(v), nil
		})
	}

	return NewKeyMarshaler(func(k K) ([]byte, error) {
		var buf bytes.Buffer

		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)

		if err := enc.Encode(k); err != nil {
			return nil, fmt.Errorf("failed to json.Encode: %w", err)
		}

		return []byte(strings.TrimRight(buf.String(), "\n")), nil
	})
}

type Transcoder[V any] interface {
	Marshaler[V]
	Unmarshal([]byte) (V, error)
}

type transcoder[V any] struct {
	marshal   func(V) ([]byte, error)
	unmarshal func([]byte) (V, error)
}

func (vt *transcoder[V]) Marshal(v V) ([]byte, error) {
	return vt.marshal(v)
}

func (vt *transcoder[V]) Unmarshal(b []byte) (V, error) {
	return vt.unmarshal(b)
}

// NewKeyMarshaler returns an instance of a Transcoder of any type
func NewTranscoder[V any](marshal func(V) ([]byte, error), unmarshal func([]byte) (V, error)) Transcoder[V] {
	return &transcoder[V]{marshal, unmarshal}
}

//nolint:gocyclo
func newDefaultTranscoder[V any]() Transcoder[V] {
	var valIsPointerType bool
	var av any
	{
		var v V

		valIsPointerType = (reflect.ValueOf(v).Type().Kind() == reflect.Pointer)
		av = any(v)
	}

	if valIsPointerType {
		switch av.(type) {
		case binaryTranscoder:
			return NewTranscoder(
				func(v V) ([]byte, error) {
					x, ok := any(v).(binaryTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					return x.MarshalBinary()
				},
				func(b []byte) (V, error) {
					var result, buf V

					x, ok := any(buf).(binaryTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					if err := x.UnmarshalBinary(b); err != nil {
						return result, err
					}

					result = buf
					return result, nil
				},
			)
		case textTranscoder:
			return NewTranscoder(
				func(v V) ([]byte, error) {
					x, ok := any(v).(textTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					return x.MarshalText()
				},
				func(b []byte) (V, error) {
					var result, buf V

					x, ok := any(buf).(textTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					if err := x.UnmarshalText(b); err != nil {
						return result, err
					}

					result = buf
					return result, nil
				},
			)
		case jsonTranscoder:
			return NewTranscoder(
				func(v V) ([]byte, error) {
					x, ok := any(v).(jsonTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					return x.MarshalJSON()
				},
				func(b []byte) (V, error) {
					var result, buf V

					x, ok := any(buf).(jsonTranscoder)
					if !ok {
						panic(errors.New("unreachable"))
					}

					if err := x.UnmarshalJSON(b); err != nil {
						return result, err
					}

					result = buf
					return result, nil
				},
			)
		}

		return NewTranscoder(
			func(v V) ([]byte, error) {
				var buf bytes.Buffer

				enc := json.NewEncoder(&buf)
				enc.SetEscapeHTML(false)

				if err := enc.Encode(v); err != nil {
					return nil, err
				}

				return buf.Bytes(), nil
			},
			func(b []byte) (V, error) {
				var result, buf V

				if err := json.Unmarshal(b, buf); err != nil {
					return result, err
				}

				result = buf
				return result, nil
			},
		)
	}

	switch av.(type) {
	case []byte:
		return NewTranscoder(
			func(v V) ([]byte, error) {
				b, ok := any(v).([]byte)
				if !ok {
					panic(errors.New("unreachable"))
				}

				if b == nil {
					return nil, nil
				}

				buf := make([]byte, len(b))
				copy(buf, b)

				return buf, nil
			},
			func(b []byte) (V, error) {
				var result V
				if b == nil {
					return result, nil
				}

				buf := make([]byte, len(b))
				copy(buf, b)

				reflect.ValueOf(&result).Elem().Set(reflect.ValueOf(buf))

				return result, nil
			},
		)
	case []rune:
		return NewTranscoder(
			func(v V) ([]byte, error) {
				runes, ok := any(v).([]rune)
				if !ok {
					panic(errors.New("unreachable"))
				}

				if runes == nil {
					return nil, nil
				}

				return []byte(string(runes)), nil
			},
			func(b []byte) (V, error) {
				var result V
				if b == nil {
					return result, nil
				}

				reflect.ValueOf(&result).Elem().Set(reflect.ValueOf([]rune(string(b))))

				return result, nil
			},
		)
	case string:
		return NewTranscoder(
			func(v V) ([]byte, error) {
				x, ok := any(v).(string)
				if !ok {
					panic(errors.New("unreachable"))
				}

				return []byte(x), nil
			},
			func(b []byte) (V, error) {
				var result V
				if b == nil {
					return result, nil
				}

				reflect.ValueOf(&result).Elem().Set(reflect.ValueOf(string(b)))

				return result, nil
			},
		)
	}

	return NewTranscoder(
		func(v V) ([]byte, error) {
			var buf bytes.Buffer

			enc := json.NewEncoder(&buf)
			enc.SetEscapeHTML(false)

			if err := enc.Encode(v); err != nil {
				return nil, err
			}

			return buf.Bytes(), nil
		},
		func(b []byte) (V, error) {
			var result, buf V

			if err := json.Unmarshal(b, &buf); err != nil {
				return result, err
			}

			result = buf
			return result, nil
		},
	)
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
		cfg.valTranscoder = newDefaultTranscoder[V]()
	}

	return &DiskCache[K, V]{
		basePath:      path,
		m:             make(map[K]diskCacheMemRecord[V], maxSize),
		keyMarshaler:  cfg.keyMarshaler,
		valTranscoder: cfg.valTranscoder,
		maxSize:       maxSize,
	}, nil
}

func (c *DiskCache[K, V]) Get(k K) (V, bool, error) { //nolint:gocritic
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
		c.size--
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
		c.size++
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
