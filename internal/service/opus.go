package service

import (
	"encoding/binary"
	"errors"
)

// transcoding constants
const (
	SampleRate     = 48000 // bits per second
	NumChannels    = 1
	SampleSize     = 960 // int16 size of each audio frame
	BytesPerInt16  = 2
	SampleMaxBytes = SampleSize * BytesPerInt16 * NumChannels
)

//nolint:gochecknoinits
func init() {
	// verify on startup SampleMaxBytes is correct

	if BytesPerInt16 != binary.Size(int16(0))/binary.Size(byte(0)) {
		panic(errors.New("BytesPerInt16 constant is wrong somehow"))
	}
}
