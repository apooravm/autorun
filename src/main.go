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
	"gopkg.in/yaml.v3"
)

var (
	AUTORUN_CFG string = "autorun.yaml"
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

/**
target root path = root_dir
extension files to look out for = exts
run command = cmd
**/

type AutorunConfig struct {
	Target_dir string   `yaml:"target_dir"`
	Exts       []string `yaml:"exts"`
	Cmd        string   `yaml:"cmd"`
}

func GenerateAutoRunConfig(path string) error {
	cfg := AutorunConfig{
		Target_dir: "path",
		Exts:       []string{"go"},
		Cmd:        "go run main.go",
	}

	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}

	if err = os.WriteFile(AUTORUN_CFG, data, 0644); err != nil {
		return err
	}

	return nil
}

func ReadAutorunConfig(path string) (*AutorunConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Println("E:Importing yaml file", err.Error())
		return nil, err
	}

	var cfg AutorunConfig
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Println("E:Unmarshalling YAML", err.Error())
		return nil, err
	}

	return &cfg, nil
}

func main() {
	// Add a path.
	args := os.Args
	if len(args) < 2 {
		// Assume autorun cfg exists in curr dir
		cfg, err := ReadAutorunConfig(AUTORUN_CFG)
		if err != nil {
			log.Println("E:Reading config YAML", err.Error())
			return
		} else {
			// run...
			StartWatcher(cfg)
		}

		return
	}

	switch os.Args[1] {
	case "generate":
		var t_path string = "./"
		if len(os.Args) < 3 {
			t_path = os.Args[2]
		}

		t_path = filepath.Join(t_path, AUTORUN_CFG)
		if err := GenerateAutoRunConfig(t_path); err != nil {
			fmt.Println("E:Generating config YAML", err.Error())
			return
		}
		fmt.Println("Created at", t_path)
		return

	default:
		fmt.Println("...")
		return
	}
}

func StartWatcher(cfg *AutorunConfig) {
	dirs, err := getSubDirs(cfg.Target_dir)
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

	cmd_str := fmt.Sprintf("cd %s && %s", cfg.Target_dir, cfg.Cmd)
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
			for _, ext := range cfg.Exts {
				if strings.HasSuffix(e.Name, "."+ext) {
					trigger <- struct{}{}
				}
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
