package webcli

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/igolaizola/musikai/pkg/webcli/view"
	"github.com/peterbourgon/ff/v3/ffcli"
	"github.com/pkg/browser"
	"gopkg.in/yaml.v3"
)

type parsedCommand struct {
	Command     *ffcli.Command
	Fields      []view.Field
	Name        string
	Description string
}

// Server serves the webcli server.
func Serve(ctx context.Context, app string, cmds []*ffcli.Command, customPort int) error {
	// Get all commands including subcommands
	parsed := parseCommands(cmds, "")

	// Create lookup and name list
	var cmdLookup = map[string]*parsedCommand{}
	var cmdNames []string
	for _, cmd := range parsed {
		cmdLookup[cmd.Name] = cmd
		cmdNames = append(cmdNames, cmd.Name)
	}

	// Multiplexer for the server
	mux := http.NewServeMux()

	// Index page handler
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("HX-Push-Url", "/")
		var cmds []view.CommandEntry
		for _, name := range cmdNames {
			cmds = append(cmds, view.CommandEntry{
				Name:        name,
				Description: cmdLookup[name].Description,
			})
		}
		v := view.List(app, cmds)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("couldn't render view:", err)
		}
	}))

	// Command page handler
	for name, cmd := range cmdLookup {
		path := fmt.Sprintf("/commands/%s", name)
		mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fields := cmd.Fields
			config := fmt.Sprintf("cfg/%s.yaml", name)
			if _, err := os.Stat(config); err == nil {
				b, err := os.ReadFile(config)
				if err != nil {
					log.Println("couldn't read config file:", err)
				}
				cfg := map[string]yaml.Node{}
				if err := yaml.Unmarshal(b, &cfg); err != nil {
					log.Println("couldn't unmarshal config file:", err)
				}
				for i, field := range fields {
					if v, ok := cfg[field.Name]; ok {
						fields[i].Default = v.Value
					}
				}
			}
			w.Header().Set("HX-Push-Url", path)
			v := view.Form(app, name, fields)
			if err := v.Render(r.Context(), w); err != nil {
				log.Println("couldn't render view:", err)
			}
		}))
	}

	var lck sync.Mutex
	processes := map[string]*process{}

	// Event stream handler
	mux.Handle("/events/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get process ID
		id := r.PathValue("id")
		if id == "" {
			httpError(w, "id is empty", http.StatusBadRequest)
			return
		}
		lck.Lock()
		proc, ok := processes[id]
		lck.Unlock()

		// If the process doesn't exist, return a close event
		if !ok {
			fmt.Fprint(w, "event: close\ndata: <div></div>\n\n")
			return
		}

		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		// Create a channel to send data
		dataC := make(chan string)
		defer close(dataC)

		// Create a context for handling client disconnection
		_, cancel := context.WithCancel(r.Context())
		defer cancel()

		// Generate a random ID
		subID := fmt.Sprintf("%d", time.Now().UnixNano())

		// Subscribe to the process logs
		proc.Subscribe(subID, func(text string, close bool) {
			text = strings.ReplaceAll(text, "\n", "<br>")
			text = fmt.Sprintf("event: log\ndata: %s\n\n", text)
			dataC <- text
			if close {
				text = "event: close\ndata: <div></div>\n\n"
				dataC <- text
				cancel()
			}
		})
		defer proc.Unsubscribe(subID)

		// Send event data to the client
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.Context().Done():
				return
			case data := <-dataC:
				fmt.Fprint(w, data)
				w.(http.Flusher).Flush()
			}
		}
	}))

	// Log list page handler
	mux.Handle("/logs", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("HX-Push-Url", "/logs")
		var logs []view.LogEntry
		for id, p := range processes {
			logs = append(logs, view.LogEntry{
				ID:       id,
				Command:  p.command,
				Start:    p.start,
				End:      p.end,
				Error:    p.error,
				Canceled: p.canceled,
			})
		}
		v := view.ListLog(app, logs)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("couldn't render view:", err)
		}
	}))

	// Log page handler
	logHandler := func(w http.ResponseWriter, r *http.Request, cancel bool) {
		// Get process ID
		id := r.PathValue("id")
		proc, ok := processes[id]
		if !ok {
			httpError(w, "reader not found", http.StatusNotFound)
			return
		}
		if cancel {
			proc.cancel()
		}
		w.Header().Set("HX-Push-Url", fmt.Sprintf("/logs/%s", id))
		v := view.Log(app, id, proc.Logs())
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("couldn't render view:", err)
		}
	}
	mux.Handle("/logs/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logHandler(w, r, false)
	}))

	// Cancel command handler
	mux.Handle("/cancel/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logHandler(w, r, true)
	}))

	// Command run handler
	mux.Handle("/run", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only post method is allowed
		if r.Method != http.MethodPost {
			httpError(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse form
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Execute command
		cmdName := r.FormValue("command")
		if cmdName == "" {
			httpError(w, "command field is empty", http.StatusBadRequest)
			return
		}
		cmd, ok := cmdLookup[r.FormValue("command")]
		if !ok {
			httpError(w, "command not found", http.StatusBadRequest)
			return
		}
		cfg := map[string]yaml.Node{}
		args := []string{r.FormValue("command")}
		for k, v := range r.Form {
			if k == "command" {
				continue
			}
			value := v[0]
			switch value {
			case "on":
				value = "true"
			case "off":
				value = "false"
			}
			args = append(args, fmt.Sprintf("-%s=%s", k, value))
			if v[0] == "" {
				continue
			}
			cfg[k] = yaml.Node{
				Value: v[0],
			}
		}
		// Save values to YAML
		/*b, err := yaml.Marshal(cfg)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}*
		if err := os.WriteFile(fmt.Sprintf("cfg/%s.yaml", cmdName), b, 0644); err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}*/
		if err := cmd.Command.Parse(args); err != nil {
			httpError(w, err.Error(), http.StatusBadRequest)
			return
		}
		id := strings.Replace(time.Now().Format("20060102-150405.999"), ".", "-", 1)
		proc, err := newProcess(ctx, args)
		if err != nil {
			httpError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lck.Lock()
		processes[id] = proc
		lck.Unlock()

		// Replace URL
		w.Header().Set("HX-Push-Url", fmt.Sprintf("/logs/%s", id))
		// Log page
		v := view.Log(app, id, proc.logs)
		if err := v.Render(r.Context(), w); err != nil {
			log.Println("couldn't render view:", err)
		}
	}))

	// Create an HTTP server with the custom ServeMux
	server := &http.Server{
		Handler: mux,
	}

	// Create a listener on a random port
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", customPort))
	if err != nil {
		return fmt.Errorf("failed to listen on a port: %v", err)
	}
	defer listener.Close()

	// Extract the port from the listener address
	port := listener.Addr().(*net.TCPAddr).Port

	// Log the port that the server is listening on
	log.Printf("Server is listening on %s", listener.Addr().String())

	// Start the server in a goroutine so that it doesn't block
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	// Open the browser to the server URL if the port is not custom
	u := fmt.Sprintf("http://localhost:%d", port)
	if customPort == 0 {
		if err := browser.OpenURL(u); err != nil {
			log.Println("failed to open browser:", err)
		}
	}
	fmt.Println("Open the following URL in your browser:", u)

	// Wait for the context to be done
	<-ctx.Done()

	// Shutdown the server when the context is done
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown failed: %v", err)
	}

	return nil
}

func parseCommands(cmds []*ffcli.Command, parent string) []*parsedCommand {
	var all []*parsedCommand
	for _, cmd := range cmds {
		p := parseCommand(cmd, parent)
		if p != nil {
			all = append(all, p)
		}
		if cmd.Subcommands != nil {
			all = append(all, parseCommands(cmd.Subcommands, p.Name)...)
		}
	}
	return all
}

func parseCommand(cmd *ffcli.Command, parent string) *parsedCommand {
	name := cmd.Name
	if parent != "" {
		name = fmt.Sprintf("%s/%s", parent, name)
	}
	parsed := &parsedCommand{
		Name:        name,
		Description: cmd.ShortHelp + "\n" + cmd.LongHelp,
		Command:     cmd,
	}
	fs := cmd.FlagSet
	if fs == nil {
		// If it doesn't have flags, it's just a holder of subcommands
		if len(cmd.Subcommands) > 0 {
			return nil
		}
		return parsed
	}
	fields := []view.Field{}
	cmd.FlagSet.VisitAll(func(f *flag.Flag) {
		t := fmt.Sprintf("%T", f.Value)
		field := view.Field{
			Name:        f.Name,
			Default:     f.DefValue,
			Description: f.Usage,
		}
		switch t {
		case "*flag.boolValue":
			field.Type = view.Boolean
		case "*flag.durationValue":
			field.Type = view.Text
		case "*flag.float64Value":
			field.Type = view.Number
		case "*flag.intValue", "*flag.int64Value":
			field.Type = view.Number
		case "*flag.stringValue":
			field.Type = view.Text
		case "*flag.uintValue", "*flag.uint64Value":
			field.Type = view.Number
		default:
			field.Type = view.Text
		}
		fields = append(fields, field)
	})
	parsed.Fields = fields
	return parsed
}

func httpError(w http.ResponseWriter, msg string, code int) {
	log.Println(msg)
	http.Error(w, msg, code)
}
