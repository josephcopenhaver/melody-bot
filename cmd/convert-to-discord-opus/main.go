package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"io"
	"os"

	"github.com/josephcopenhaver/discord-bot/internal/service"
	"github.com/rs/zerolog/log"
)

func main() {
	var inputFile, outputFile string

	flag.StringVar(&inputFile, "i", "", "specifies input file, if unspecified uses stdin")
	flag.StringVar(&outputFile, "o", "", "specifies output file, if unspecified uses stdout")

	flag.Parse()

	err := convert(inputFile, outputFile)
	if err != nil {
		log.Fatal().
			Err(err).
			Str("input_file", inputFile).
			Str("output_file", outputFile).
			Msg("failed to convert file")
	}
}

// convert:
//
// inputFile param is expected to be pcm_s16le formatted
//
// outputFile param will be discord-opus packet formatted
func convert(inputFile string, outputFile string) error {

	var in *bufio.Reader
	{
		var reader io.Reader
		if inputFile != "" {

			f, err := os.Open(inputFile)
			if err != nil {
				return err
			}
			defer f.Close()

			reader = f
		} else {
			reader = os.Stdin
		}

		in = bufio.NewReaderSize(bufio.NewReader(reader), 2*(service.SampleSize*service.NumChannels))
	}

	var writer io.Writer
	if outputFile != "" {

		f, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE, 0664)
		if err != nil {
			return err
		}
		defer f.Close()

		writer = f
	} else {
		writer = os.Stdout
	}

	opusWriter, err := service.NewOpusWriter(writer)
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
