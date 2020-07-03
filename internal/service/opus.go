package service

import (
	"bufio"
	"encoding/binary"
	"io"

	"github.com/josephcopenhaver/gopus"
	"github.com/rs/zerolog/log"
)

// transcoding constants
const (
	SampleRate       = 48000 // kits per second
	NumChannels      = 1
	SampleSize       = 960 // int16 size of each audio frame
	SampleMaxBytes   = SampleSize * 2 * NumChannels
	NumPacketBuffers = 4 // should always be 2 greater than the OpusSend channel packet size to ensure no buffer lag occurs and no corruption occurs, this also avoids allocations and reduces CPU burn
)

var NumBytesForOpusPacketLength int

func init() {
	if SampleMaxBytes > 0xffffffff {
		NumBytesForOpusPacketLength = 8
	} else if SampleMaxBytes > 0xffff {
		NumBytesForOpusPacketLength = 4
	} else if SampleMaxBytes > 0xff {
		NumBytesForOpusPacketLength = 2
	} else if SampleMaxBytes > 0 {
		NumBytesForOpusPacketLength = 1
	} else {
		log.Fatal().Msg("sample size or num channels: too high or zero")
	}
}

type OpusWriter struct {
	f             *bufio.Writer
	packetSizeBuf []byte
	packetBuf     []byte
	opusEncoder   *gopus.Encoder
}

func NewOpusWriter(f io.Writer) (*OpusWriter, error) {
	opusEncoder, err := gopus.NewEncoder(SampleRate, NumChannels, gopus.Audio)
	if err != nil {
		return nil, err
	}

	return &OpusWriter{
		f:             bufio.NewWriterSize(bufio.NewWriter(f), 2*(SampleMaxBytes+NumBytesForOpusPacketLength)),
		packetSizeBuf: make([]byte, binary.Size(uint64(0))),
		packetBuf:     make([]byte, SampleMaxBytes),
		opusEncoder:   opusEncoder,
	}, nil
}

// Write Packet input
func (o *OpusWriter) WritePacket(inputS16lePacket []int16) error {

	numBytes, err := o.opusEncoder.Encode(inputS16lePacket, SampleSize, o.packetBuf)
	if err != nil {
		return err
	}

	if numBytes == 0 {
		panic("when the hell would this happen?")
	}

	binary.LittleEndian.PutUint64(o.packetSizeBuf, uint64(numBytes))

	err = binary.Write(o.f, binary.LittleEndian, o.packetSizeBuf[:NumBytesForOpusPacketLength])
	if err != nil {
		return err
	}

	err = binary.Write(o.f, binary.LittleEndian, o.packetBuf[:numBytes])
	if err != nil {
		return err
	}

	return nil
}

func (o *OpusWriter) Flush() error {
	return o.f.Flush()
}

type OpusReader struct {
	f             *bufio.Reader
	packetSizeBuf []byte
}

func NewOpusReader(f io.Reader) *OpusReader {
	return &OpusReader{
		f:             bufio.NewReaderSize(bufio.NewReader(f), NumPacketBuffers*(SampleMaxBytes+NumBytesForOpusPacketLength)),
		packetSizeBuf: make([]byte, binary.Size(uint64(0))),
	}
}

func (o *OpusReader) ReadPacket(output []byte) (int, error) {

	{
		subSlice := o.packetSizeBuf[:NumBytesForOpusPacketLength]

		err := binary.Read(o.f, binary.LittleEndian, &subSlice)
		if err != nil {
			return 0, err
		}
	}

	numBytes := binary.LittleEndian.Uint64(o.packetSizeBuf)
	if numBytes == 0 {
		return 0, nil
	}

	{
		subSlice := output[:numBytes]

		err := binary.Read(o.f, binary.LittleEndian, &subSlice)
		if err != nil {
			return 0, err
		}
	}

	return int(numBytes), nil
}
