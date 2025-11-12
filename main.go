package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"
)

type config struct {
	appRoot                 string
	pandocFontsPath         string
	pandocTypstTemplatePath string
	indexPath               string
	pandocExecutable        string
	pandocTimeout           time.Duration
	port                    int
	host                    string
}

type contextKey string

const (
	keyCfg contextKey = "cfg"
)

const resourcesDirName = "resources"
const indexSubPath = "static/index.html"
const pandocFontsSubPath = "pandoc/fonts"
const pandocTypstTemplateSubPath = "pandoc/templates/typst.template"

var teamIdLookup = map[string]string{
	"team-esm":  "E-Sport-Management",
	"team-hs":   "Hochschulen",
	"team-oea":  "Öffentlichkeitsarbeit",
	"team-tech": "Technik",
	"team-vs":   "Veranstaltungen",
	"team-vh":   "Vereinsheim",
}

func tryDeleteFile(f *os.File) {
	if err := os.Remove(f.Name()); err != nil {
		log.Printf("failed to delete temporary file: %v\n", err)
	}
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	cfg, ok := r.Context().Value(keyCfg).(config)
	if !ok {
		log.Fatal("context did not provide application config")
	}

	if r.Method == "GET" {
		http.ServeFile(w, r, cfg.indexPath)
		return
	}

	if r.Method == "POST" {
		err := r.ParseMultipartForm(32 << 20) // 32 MB
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("failed to parse multipart request body: %v\n", err)
			return
		}

		teamIdSlice, ok := r.MultipartForm.Value["team"]
		if !ok || len(teamIdSlice) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		teamId := teamIdSlice[0]
		teamName, ok := teamIdLookup[teamId]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			log.Printf("invalid team identifier: %v\n", teamId)
		}

		tmpIn, err := os.CreateTemp("", "pandoc-input-")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("failed to create temporary file: %v\n", err)
			return
		}

		log.Printf("created temporary input file at %v\n", tmpIn.Name())
		defer tryDeleteFile(tmpIn)

		mdFile, _, err := r.FormFile("md-file")
		// check if an error occurred while reading the md-file form field
		if err != nil {
			// if it's any error other than "missing file", abort
			if !errors.Is(err, http.ErrMissingFile) {
				w.WriteHeader(http.StatusInternalServerError)
				log.Printf("failed to read file: %v\n", err)
				return
			}

			// at this point it's clear that md-file wasn't provided, so try md-content next
			mdContentSlice, ok := r.MultipartForm.Value["md-content"]
			if !ok || len(mdContentSlice) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}

			mdContent := mdContentSlice[0]

			// attempt to write to temporary file
			if _, err := tmpIn.WriteString(mdContent); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				log.Printf("failed to write contents to temporary file: %v\n", err)
				return
			}
		} else {
			// md-file is present
			if _, err := io.Copy(tmpIn, mdFile); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				log.Printf("failed to write contents to temporary file: %v\n", err)
				return
			}
		}

		tmpOut, err := os.CreateTemp("", "pandoc-output-")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("failed to create temporary file: %v\n", err)
			return
		}

		log.Printf("created temporary output file at %v\n", tmpOut.Name())
		defer tryDeleteFile(tmpOut)

		cmdCtx, cmdCtxCancel := context.WithTimeout(r.Context(), cfg.pandocTimeout)
		defer cmdCtxCancel()

		cmd := exec.CommandContext(cmdCtx, cfg.pandocExecutable, tmpIn.Name(), "-f", "markdown", "-o", tmpOut.Name(), "-t", "pdf", "--template", cfg.pandocTypstTemplatePath, "-V", fmt.Sprintf("team=%v", teamName), "--pdf-engine", "typst", "--pdf-engine-opt", "--pdf-standard=a-2b")
		cmd.Env = append(cmd.Environ(), fmt.Sprintf("TYPST_FONT_PATHS=%v", cfg.pandocFontsPath))

		log.Printf("executing %v", cmd.Args)

		if err := cmd.Run(); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("failed to execute: %v", err)
			return
		}

		http.ServeFile(w, r, tmpOut.Name())

		return
	}

	w.WriteHeader(http.StatusMethodNotAllowed)
}

func runServer(ctx context.Context, cfg config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex)

	srv := &http.Server{
		Addr:         fmt.Sprintf("%v:%v", cfg.host, cfg.port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		BaseContext: func(l net.Listener) context.Context {
			return context.WithValue(ctx, keyCfg, cfg)
		},
	}

	serverErr := make(chan error, 1)

	go func() {
		log.Printf("running server on %v\n", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		log.Printf("server encountered an error: %v", err)
	case sig := <-stop:
		log.Printf("received shutdown signal: %v", sig)
	}

	log.Println("attempting to shut down server gracefully")

	cancelCtx, cancel := context.WithTimeout(ctx, time.Second*15)
	defer cancel()

	if err := srv.Shutdown(cancelCtx); err != nil {
		log.Printf("failed to shut down server: %v", err)
	}
}

func handleCli(ctx context.Context, cmd *cli.Command) error {
	appRoot := cmd.String("applicationRoot")
	pandocExecutable := cmd.String("pandocExecutable")
	pandocTimeout := cmd.Duration("pandocTimeout")
	host := cmd.String("host")
	port := cmd.Int("port")

	if port <= 0 {
		return fmt.Errorf("port must be a positive number, is: %v", port)
	}

	if appRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("failed to determine working directory: %v", err)
		}

		log.Printf("no application root provided, using working directory at %v\n", cwd)
		appRoot = cwd
	}

	err := os.Mkdir(filepath.Join(appRoot, "logs"), 0750) // rwx-r-x---
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// rdwr = open read-write, create = create if not exist, append = append when writing
	f, err := os.OpenFile(filepath.Join(appRoot, "logs", "les-reportconv.log"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0660)
	if err != nil {
		return fmt.Errorf("failed to create log file: %v", err)
	}

	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("failed to close log file: %v\n", err)
		}
	}()

	logWriter := io.MultiWriter(os.Stderr, f)
	log.SetOutput(logWriter)

	runServer(ctx, config{
		appRoot:                 appRoot,
		pandocExecutable:        pandocExecutable,
		pandocTimeout:           pandocTimeout,
		port:                    port,
		host:                    host,
		pandocFontsPath:         filepath.Join(appRoot, resourcesDirName, pandocFontsSubPath),
		pandocTypstTemplatePath: filepath.Join(appRoot, resourcesDirName, pandocTypstTemplateSubPath),
		indexPath:               filepath.Join(appRoot, resourcesDirName, indexSubPath),
	})

	return nil
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Printf("failed to load .env file: %v\n", err)
	}

	cmd := &cli.Command{
		Name:   "reportconv",
		Usage:  "minimal server for converting Markdown reports to neat PDFs",
		Action: handleCli,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "applicationRoot",
				Aliases: []string{"appRoot"},
				Usage:   "path to application root directory",
				Sources: cli.EnvVars("APPLICATION_ROOT"),
			},
			&cli.StringFlag{
				Name:    "pandocExecutable",
				Usage:   "name of pandoc executable",
				Value:   "pandoc",
				Sources: cli.EnvVars("PANDOC_EXECUTABLE"),
			},
			&cli.DurationFlag{
				Name:    "pandocTimeout",
				Usage:   "timeout for pandoc conversion",
				Value:   10 * time.Second,
				Sources: cli.EnvVars("PANDOC_TIMEOUT"),
			},
			&cli.StringFlag{
				Name:    "host",
				Usage:   "host to expose service on",
				Aliases: []string{"h"},
				Value:   "0.0.0.0",
				Sources: cli.EnvVars("HTTP_HOST"),
			},
			&cli.IntFlag{
				Name:    "port",
				Usage:   "port to expose service on",
				Aliases: []string{"p"},
				Value:   3333,
				Sources: cli.EnvVars("HTTP_PORT"),
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
