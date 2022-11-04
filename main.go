package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"github.com/google/go-github/v48/github"
	"github.com/kirsle/configdir"
	"github.com/schollz/progressbar/v3"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"gerace.dev/zipfs"
	"github.com/cli/oauth"
	"github.com/urfave/cli/v2"
	"golang.org/x/oauth2"
)

const ClientID = "057154906abe87098d13"

func main() {
	app := &cli.App{
		Action: func(c *cli.Context) error {
			configPath := configdir.LocalConfig("gav")

			err := configdir.MakePath(configPath)
			if err != nil {
				return err
			}

			configFile := filepath.Join(configPath, "tok.tok")

			var token string

			if _, err := os.Stat(configFile); os.IsNotExist(err) {
				flow := &oauth.Flow{
					Host:     oauth.GitHubHost("https://github.com"),
					ClientID: ClientID,
					Scopes:   []string{"repo", "read:org"},
				}

				accessToken, err := flow.DetectFlow()
				if err != nil {
					return err
				}

				fmt.Printf("Access token: %s\n", accessToken.Token)

				token = accessToken.Token

				fh, err := os.Create(configFile)
				if err != nil {
					return err
				}

				defer fh.Close()

				_, err = fh.Write([]byte(token))
				if err != nil {
					return err
				}

				fmt.Println("Saved token")
			} else {
				tok, err := os.ReadFile(configFile)
				if err != nil {
					return err
				}

				token = string(tok)
			}

			ts := oauth2.StaticTokenSource(
				&oauth2.Token{AccessToken: token},
			)

			tc := oauth2.NewClient(c.Context, ts)

			client := github.NewClient(tc)

			dc, err := extractRunID(c.Args().Get(0))
			if err != nil {
				return err
			}

			lst, _, err := client.Actions.ListWorkflowRunArtifacts(c.Context, dc.Org, dc.Repo, dc.RunID, &github.ListOptions{
				Page:    0,
				PerPage: 25,
			})
			if err != nil {
				return err
			}

			if lst.GetTotalCount() == 0 {
				fmt.Println("No artifacts found")
			} else {
				url := lst.Artifacts[0].GetArchiveDownloadURL()

				archive, err := downloadArchive(url, token, *lst.Artifacts[0].SizeInBytes)
				if err != nil {
					return err
				}

				zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
				if err != nil {
					return err
				}

				zfs, err := zipfs.NewZipFileSystem(zr)
				if err != nil {
					return err
				}

				httpfs := http.FileServer(zfs)

				http.Handle("/", httpfs)

				log.Println("Starting server at: http://localhost:6969")

				go open("http://localhost:6969")

				err = http.ListenAndServe(":6969", nil)
				if err != nil {
					return err
				}
			}

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

type URLDeconstruct struct {
	Org   string
	Repo  string
	RunID int64
	JobID string
}

func extractRunID(url string) (*URLDeconstruct, error) {
	rx := regexp.MustCompile("https?:\\/\\/github.com\\/(.+)\\/(.+)\\/actions\\/runs\\/(\\d+)\\/?(.*)?")

	sm := rx.FindStringSubmatch(url)

	if len(sm) < 4 {
		return nil, errors.New("invalid run url")
	}

	rid, err := strconv.Atoi(sm[3])
	if err != nil {
		return nil, err
	}

	return &URLDeconstruct{
		Org:   sm[1],
		Repo:  sm[2],
		RunID: int64(rid),
	}, nil
}

func downloadArchive(url, token string, expectedSize int64) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	bar := progressbar.DefaultBytes(expectedSize, "Downloading Archive")

	bod, err := io.ReadAll(io.TeeReader(resp.Body, bar))
	if err != nil {
		return nil, err
	}

	bar.Clear()
	bar.Close()

	return bod, nil
}

// open opens the specified URL in the default browser of the user.
func open(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}
