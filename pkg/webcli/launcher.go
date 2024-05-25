package webcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type process struct {
	logs      string
	callbacks map[string]func(string, bool)
	listening context.CancelFunc
	lck       sync.Mutex
	command   string
	start     time.Time
	end       time.Time
	error     bool
	cancel    context.CancelFunc
	canceled  bool
}

func (p *process) Logs() string {
	return p.logs
}

func (p *process) Subscribe(id string, callback func(string, bool)) {
	defer p.listening()
	p.lck.Lock()
	defer p.lck.Unlock()
	p.callbacks[id] = callback
}

func (p *process) Unsubscribe(id string) {
	p.lck.Lock()
	defer p.lck.Unlock()
	delete(p.callbacks, id)
}

func newProcess(ctx context.Context, args []string) (*process, error) {
	if len(args) == 0 {
		return nil, errors.New("no command provided")
	}

	// Get the command name and arguments
	cmdName := args[0]
	parts := strings.Split(cmdName, "/")
	args = append(parts, args[1:]...)

	// Launch the process
	start := time.Now().UTC()
	ctx, cancel := context.WithCancel(ctx)
	combinedOutput, _, err := launch(ctx, args)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("error launching instance: %w", err)
	}

	// Create the process that handles the output
	waitListening, listening := context.WithCancel(ctx)
	p := &process{
		callbacks: make(map[string]func(string, bool)),
		start:     start,
		command:   cmdName,
		listening: listening,
		cancel:    cancel,
	}

	go func() {
		defer cancel()
		defer func() {
			p.end = time.Now().UTC()
		}()

		// Wait for first subscription or timeout
		select {
		case <-waitListening.Done():
		case <-time.After(500 * time.Millisecond):
		}

		for {
			select {
			case <-ctx.Done():
				if errors.Is(ctx.Err(), context.Canceled) {
					p.canceled = true
				}
				return
			default:
			}

			// Read the output of the process
			data := make([]byte, 1024)
			var text string
			n, err := combinedOutput.Read(data)
			if err != nil {
				if !errors.Is(err, io.EOF) {
					p.error = true
				}
				text = err.Error()
			} else {
				text = string(data[:n])
			}
			text = strings.ReplaceAll(text, "\n", "<br>")

			// Store the output
			p.logs += text

			// Send the output to all subscribers
			p.lck.Lock()
			for _, callback := range p.callbacks {
				callback(text, err != nil)
			}
			p.lck.Unlock()

			// Exit if the process has ended
			if err != nil {
				if errors.Is(ctx.Err(), context.Canceled) {
					p.canceled = true
				}
				return
			}
		}
	}()

	return p, nil
}

// Launch starts another instance of the current executable with provided arguments.
// It returns a single reader for both stdout and stderr, and a writer for stdin.
func launch(ctx context.Context, args []string) (combinedOutput io.Reader, stdin io.Writer, err error) {
	// Get the path to the currently running executable
	exePath, err := os.Executable()
	if err != nil {
		return nil, nil, fmt.Errorf("error getting executable path: %w", err)
	}

	// Create the command with the context and the arguments
	cmd := exec.CommandContext(ctx, exePath, args...)

	// Create a pipe for stdin
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating stdin pipe: %w", err)
	}

	// Set up a single pipe for stdout and stderr
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("error creating stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // Redirect stderr to stdout

	// Start the command
	err = cmd.Start()
	if err != nil {
		return nil, nil, fmt.Errorf("error starting command: %w", err)
	}

	// Return the combined stdout/stderr pipe and the stdin pipe
	return stdoutPipe, stdinPipe, nil
}
