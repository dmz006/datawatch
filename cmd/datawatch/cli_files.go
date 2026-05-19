// BL333 (v8.3.0 T43) — `datawatch files` CLI subcommands for the federated
// file service.
//
//	datawatch files list [--path <path>]
//	datawatch files upload <local-file> [--remote-path <path>]
//	datawatch files delete <remote-path>
//	datawatch files peer <peer-name> [--path <path>]
//	datawatch files meta

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newFilesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files",
		Short: "Manage federated file service (BL333)",
		Long: `Browse, upload, and delete files in the datawatch federated file service.

Subcommands:
  list [--path <path>]                    List files at path (default: service root)
  upload <local-file> [--remote-path <p>] Upload a local file to the service
  delete <remote-path>                    Delete a file from the service
  peer <peer-name> [--path <p>]           List files in a peer's subdirectory
  meta                                    Show storage overview (root, peer/discussion counts)`,
	}
	cmd.AddCommand(
		newFilesListCmd(),
		newFilesUploadCmd(),
		newFilesDeleteCmd(),
		newFilesPeerCmd(),
		newFilesMetaCmd(),
	)
	return cmd
}

func newFilesListCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List files at path",
		RunE: func(_ *cobra.Command, _ []string) error {
			q := url.Values{}
			if path != "" {
				q.Set("path", path)
			}
			ep := "/api/files"
			if len(q) > 0 {
				ep += "?" + q.Encode()
			}
			return daemonGet(ep)
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "directory path to list (default: service root)")
	return cmd
}

func newFilesUploadCmd() *cobra.Command {
	var remotePath string
	cmd := &cobra.Command{
		Use:   "upload <local-file>",
		Short: "Upload a local file to the file service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			localFile := args[0]
			f, err := os.Open(localFile)
			if err != nil {
				return fmt.Errorf("open %s: %w", localFile, err)
			}
			defer f.Close() //nolint:errcheck

			dest := remotePath
			if dest == "" {
				dest = filepath.Base(localFile)
			}

			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			if err := mw.WriteField("path", dest); err != nil {
				return err
			}
			fw, err := mw.CreateFormFile("file", filepath.Base(localFile))
			if err != nil {
				return err
			}
			if _, err := io.Copy(fw, f); err != nil {
				return err
			}
			mw.Close() //nolint:errcheck

			req, err := http.NewRequest(http.MethodPost, daemonURL()+"/api/files", &buf)
			if err != nil {
				return err
			}
			req.Header.Set("Content-Type", mw.FormDataContentType())
			if tok := daemonToken(); tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			resp, err := daemonClient().Do(req)
			if err != nil {
				return fmt.Errorf("daemon not reachable: %w", err)
			}
			defer resp.Body.Close() //nolint:errcheck
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode/100 != 2 {
				return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			}
			prettyPrint(body)
			return nil
		},
	}
	cmd.Flags().StringVar(&remotePath, "remote-path", "", "destination path in the file service (default: filename only)")
	return cmd
}

func newFilesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <remote-path>",
		Short: "Delete a file from the file service",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonJSON(http.MethodDelete, "/api/files", map[string]any{
				"path": args[0],
			})
		},
	}
}

func newFilesPeerCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "peer <peer-name>",
		Short: "List files in a peer's subdirectory",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			ep := "/api/files/peers/" + url.PathEscape(args[0])
			if path != "" {
				ep += "?path=" + url.QueryEscape(path)
			}
			return daemonGet(ep)
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "subdirectory within the peer directory")
	return cmd
}

func newFilesMetaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "meta",
		Short: "Show file service storage overview",
		RunE: func(_ *cobra.Command, _ []string) error {
			return daemonGet("/api/files/meta")
		},
	}
}

// filesUploadJSON sends a JSON-body upload (used internally for small text files).
func filesUploadJSON(destPath, content string) error {
	body, _ := json.Marshal(map[string]string{
		"path":    destPath,
		"content": content,
	})
	req, err := http.NewRequest(http.MethodPost, daemonURL()+"/api/files/upload", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := daemonToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := daemonClient().Do(req)
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	prettyPrint(respBody)
	return nil
}
