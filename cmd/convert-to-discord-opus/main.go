package main

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/rs/zerolog/log"
)

func main() {

	err := convert(os.Args[1], os.Args[2])
	if err != nil {
		log.Fatal().
			Err(err).
			Str("input_file", os.Args[1]).
			Str("output_file", os.Args[2]).
			Msg("failed to convert file")
	}
}

// convert:
//
// inputFile param is expected to be pcm_s16le formatted
//
// outputFile param will be discord-opus packet formatted
func convert(inputFile string, outputFile string) error {

	in, err := os.Open(inputFile)
	if err != nil {
		return err
	}

	defer in.Close()

	out, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE, 0664)
	if err != nil {
		return err
	}

	defer out.Close()

	opusWriter, err := service.NewOpusWriter(out)
	if err != nil {
		return err
	}

	inBufArray := [service.SampleSize * service.NumChannels]int16{}
	inBuf := inBufArray[:]

	for {

		err = binary.Read(in, binary.LittleEndian, &inBuf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}

		err = opusWriter.WritePacket(inBuf)
		if err != nil {
			return err
		}
	}

	return opusWriter.Flush()
}
