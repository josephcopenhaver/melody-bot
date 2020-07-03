package service

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/rs/zerolog/log"
)

// SetNiceness accepts a process niceness from -20 to 19
//
// the lower the niceness score, the more CPU time the process is granted
func SetNiceness(niceness int) error {

	cmd := exec.Command("renice", "-n", strconv.Itoa(niceness), "-p", strconv.Itoa(os.Getpid()))

	// TODO: capture and log instead
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	err := cmd.Run()
	if err != nil {
		log.Err(err).
			Int("niceness", niceness).
			Msg("failed to set process niceness")

		return fmt.Errorf("failed to set process niceness: %v", err)
	}

	return nil
}
