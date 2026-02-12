package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Runner struct {
	cmd    string // main command, eg: go run main.go
	proc   *exec.Cmd
	cancel context.CancelFunc
	done   chan error
}

func (r *Runner) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	// CommandContext ties the lifecycle of the process to the context
	// when r.cancel is called, SIGTERM is sent
	r.proc = exec.CommandContext(ctx, "sh", "-c", r.cmd)

	// following attach the child process to the terminal
	// output shows in terminal, ctrl+c works, interactive programs work
	r.proc.Stdout = os.Stdout
	r.proc.Stderr = os.Stderr
	r.proc.Stdin = os.Stdin

	r.done = make(chan error, 1)

	if err := r.proc.Start(); err != nil {
		return err
	}

	go func() {
		r.done <- r.proc.Wait()
	}()

	return nil
}

func (r *Runner) Stop() error {
	if r.cancel == nil {
		return nil
	}

	r.cancel() // send kill signal

	// wait for the process to exit for 3s
	// else kill it and move on
	select {
	case err := <-r.done: // wait for process to exit
		return err

	case <-time.After(3 * time.Second):
		return r.proc.Process.Kill()
	}
}

func getSubDirs(rootpath string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(rootpath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			dirs = append(dirs, path)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return dirs, nil
}

func main() {
	// Add a path.
	args := os.Args
	if len(args) < 2 {
		log.Println("no root path provided")
		return
	}

	dirs, err := getSubDirs(args[1])
	if err != nil {
		fmt.Println("Error", err.Error())
		return
	}

	watcher, _ := fsnotify.NewWatcher()
	defer watcher.Close()

	for _, d := range dirs {
		err = watcher.Add(d)
		if err != nil {
			log.Fatal(err)
		}
	}

	cmd_str := fmt.Sprintf("cd %s && go run main.go", args[1])
	runner := &Runner{cmd: cmd_str}
	runner.Start()

	// trigger to notify restarting
	trigger := make(chan struct{})
	// if events keep happening -> do nothing
	// if they stop for 300ms, trigger restart
	restart := debounce(trigger, 300*time.Millisecond)

	// validation loop, runs forever in bg
	go func() {
		for e := range watcher.Events {
			if strings.HasSuffix(e.Name, ".go") {
				trigger <- struct{}{}
			}
		}
	}()

	// main restart loop
	for range restart {
		fmt.Printf("Restarting...\n\n")
		runner.Stop()
		runner.Start()
	}
}

// keeps resetting the timer as the events keep coming in
// once the timer finishes the delay amount, fire a single out channel
func debounce(ch <-chan struct{}, delay time.Duration) <-chan struct{} {
	out := make(chan struct{}, 1)
	go func() {
		var t *time.Timer
		for range ch {
			// if a previous timer is running, stop (cancel) it
			if t != nil {
				t.Stop()
			}
			t = time.AfterFunc(delay, func() {
				out <- struct{}{}
			})
		}
	}()
	return out
}

func main2() {
	// Create new watcher.
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	// Start listening for events.
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				// log.Println("event:", event)
				if event.Has(fsnotify.Write) {
					log.Println("modified file:", event.Name)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	// Add a path.
	args := os.Args
	if len(args) < 2 {
		log.Println("no path provided")
		return
	}

	fmt.Println("Watching ", args[1])

	err = watcher.Add(args[1])
	if err != nil {
		log.Fatal(err)
	}

	// Block main goroutine forever.
	<-make(chan struct{})
}
