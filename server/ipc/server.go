package ipcserver

import (
	"errors"
	"sync"
	"time"

	"github.com/iotaledger/giota"
	"github.com/muxxer/diverdriver/logs"
)

var (
	powMutex   = &sync.Mutex{}
	powFuncPtr giota.PowFunc
)

// SetPowFunc sets the function pointer for POW
func SetPowFunc(f giota.PowFunc) {
	powFuncPtr = f
}

// powFunc calls the hardware POW secured by a Mutex
func powFunc(trytes giota.Trytes, mwm int) (giota.Trytes, error) {
	powMutex.Lock()
	defer powMutex.Unlock()

	if powFuncPtr == nil {
		return "", errors.New("powFunc not initialized")
	}

	logs.Log.Debugf("Starting PoW! Weight: %d", mwm)
	ts := time.Now()
	result, err := powFuncPtr(trytes, mwm)
	logs.Log.Debugf("Finished PoW! Time: %d [ms]", (int64(time.Since(ts) / time.Millisecond)))

	return result, err
}
