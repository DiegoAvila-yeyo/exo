package agenthost

import (
	"fmt"
	"io"
	"os"
	"sync"
)

var stdoutSwapMu sync.Mutex

func redirectStdout(dst io.Writer) (func(), error) {
	stdoutSwapMu.Lock()

	reader, writer, err := os.Pipe()
	if err != nil {
		stdoutSwapMu.Unlock()
		return nil, fmt.Errorf("capture agent stdout: %w", err)
	}

	original := os.Stdout
	os.Stdout = writer

	var copyWG sync.WaitGroup
	copyWG.Add(1)
	go func() {
		defer copyWG.Done()
		_, _ = io.Copy(dst, reader)
		_ = reader.Close()
	}()

	return func() {
		_ = writer.Close()
		os.Stdout = original
		copyWG.Wait()
		stdoutSwapMu.Unlock()
	}, nil
}
