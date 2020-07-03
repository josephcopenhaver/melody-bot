package service

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"
)

var nicenessMutex sync.Mutex
var nicenessInitialized int32
var nicenessCanChange int32

func NicenessInit() {
	nicenessMutex.Lock()
	defer nicenessMutex.Unlock()

	if atomic.LoadInt32(&nicenessInitialized) != 0 {
		return
	}
	atomic.StoreInt32(&nicenessInitialized, 1)

	err := nicenessCheckCanChange()
	if err != nil {
		log.Error().
			Err(err).
			Msg("cannot lower niceness, missing SYS_NICE docker capability?: this process is unable to adjust it's niceness")
		return
	}

	atomic.StoreInt32(&nicenessCanChange, 1)
}

func init() {
	NicenessInit()
}

func nicenessCurrentValue() (int, error) {
	var result int

	processStatFile := "/proc/" + strconv.Itoa(os.Getpid()) + "/stat"

	f, err := os.Open(processStatFile)
	if err != nil {
		return result, err
	}

	defer f.Close()

	var line string
	{
		scanner := bufio.NewScanner(f)
		if scanner.Scan() {
			line = scanner.Text()
		}

		err = scanner.Err()
		if err != nil {
			return result, err
		}
	}

	parts := strings.Split(line, ")")
	if len(parts) > 1 {
		line = strings.TrimSpace(parts[1])
	}

	// normally index 18 on linux systems, but split early to remove the process head, so 16 is the new real index
	indexOfNiceness := 18 - 2

	parts = strings.Split(line, " ")

	var nicenessStr string
	if len(parts) > indexOfNiceness {
		nicenessStr = parts[indexOfNiceness]
	}

	if nicenessStr == "" {
		return result, errors.New("failed to find current process niceness")
	}

	sysNiceness, err := strconv.ParseUint(nicenessStr, 10, 64)
	if err != nil {
		return result, err
	}

	// support systems where niceness is 32 bits
	if (sysNiceness & 0x80000000) != 0 {
		sysNiceness |= 0xffffffff00000000
	}

	result = int(sysNiceness)

	if result > 19 || result < -20 {
		return 0, errors.New("invalid niceness read from process filesystem")
	}

	return result, nil
}

func nicenessCheckCanChange() error {

	// get process current niceness
	originalNiceness, err := nicenessCurrentValue()
	if err != nil {
		return err
	}
	niceness := originalNiceness

	rangeTest := []int{
		-20,
		19,
	}

	if rangeTest[0] == niceness {
		rangeTest[0] = -19
		rangeTest[1] = -20
		rangeTest = append(rangeTest, 19)
	}

	for _, niceness = range rangeTest {
		err = setNiceness(niceness)
		if err != nil {
			return err
		}
	}

	if niceness != originalNiceness {
		err = setNiceness(originalNiceness)
		if err != nil {
			return err
		}
	}

	return nil
}

// SetNiceness accepts a process niceness from -20 to 19
//
// the lower the niceness score, the more CPU time the process is granted
func SetNiceness(niceness int) error {
	nicenessMutex.Lock()
	defer nicenessMutex.Unlock()

	return setNiceness(niceness)
}

// setNiceness accepts a process niceness from -20 to 19
//
// the lower the niceness score, the more CPU time the process is granted
//
// NOT THREAD SAFE: See SetNiceness()
func setNiceness(niceness int) error {

	if atomic.LoadInt32(&nicenessCanChange) != 0 {
		return nil
	}

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

// FreezeNiceness is not thread safe
func FreezeNiceness() error {
	nicenessMutex.Lock()
	defer nicenessMutex.Unlock()

	if atomic.LoadInt32(&nicenessCanChange) == 0 {
		return nil
	}
	atomic.StoreInt32(&nicenessCanChange, 0)

	return setNiceness(0)
}
