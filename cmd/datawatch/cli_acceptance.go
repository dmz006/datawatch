// `datawatch acceptance`: the "done when" lists, through the daemon only.
//
//	datawatch acceptance list
//	datawatch acceptance show <request>
//	datawatch acceptance edit <request> [--source s] [--ask text] [--add-ask text] [--items file.json] [--remove id]
//	datawatch acceptance freeze <request> --hash <hash>
//	datawatch acceptance confirm <request> <item|list> --answer yes|no --words text
//	datawatch acceptance accept <request> <item> --reason text --words text
//	datawatch acceptance report | catalog

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// acceptanceCLIBody starts a write body. Run inside a session, the command
// line says which one, so a doer's own answer never counts. The fields can
// only lower how much an answer counts.
func acceptanceCLIBody(id, words string) map[string]any {
	body := map[string]any{"id": id, "actor": "cli", "via": "cli"}
	if sid := strings.TrimSpace(os.Getenv("DATAWATCH_SESSION_ID")); sid != "" {
		body["via_session"] = sid
	}
	if words != "" {
		body["words"] = words
	}
	return body
}

func setIfAny(body map[string]any, key, v string) {
	if strings.TrimSpace(v) != "" {
		body[key] = v
	}
}

func newAcceptanceCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "acceptance",
		Aliases: []string{"done-when"},
		Short:   `"Done when" lists: what each request must meet before it reads done`,
		Long: `Each request can carry a "done when" list. It is fixed before work starts,
and every item reads met, not met or cannot tell. The setting
acceptance.mode must be "report"; nothing is blocked because of a list.`,
	}
	get := func(use, short, path string) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs,
			RunE: func(*cobra.Command, []string) error { return daemonGet(path) }}
	}
	show := &cobra.Command{Use: "show <request>", Short: "Show one list and how it reads", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return daemonGet("/api/acceptance?" + url.Values{"id": {args[0]}}.Encode())
		}}

	var source, ask, addAsk, itemsFile, neededBy, words, hash string
	var remove, packs []string
	edit := &cobra.Command{Use: "edit <request>", Short: "Create a list or change one (changes to a fixed list are recorded)", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			body := acceptanceCLIBody(args[0], words)
			setIfAny(body, "source", source)
			setIfAny(body, "ask", ask)
			setIfAny(body, "add_ask", addAsk)
			setIfAny(body, "needed_by", neededBy)
			setIfAny(body, "hash", hash)
			if len(remove) > 0 {
				body["remove"] = remove
			}
			if len(packs) > 0 {
				body["packs"] = packs
			}
			if itemsFile != "" {
				raw, err := os.ReadFile(itemsFile) // #nosec G304 -- file named by the person running the command
				if err != nil {
					return err
				}
				var items []json.RawMessage
				if err := json.Unmarshal(raw, &items); err != nil {
					return fmt.Errorf("--items must be a JSON array: %w", err)
				}
				body["items"] = items
			}
			return daemonJSON(http.MethodPost, "/api/acceptance/edit", body)
		}}
	edit.Flags().StringVar(&source, "source", "", "New list: where the request came from (message, report, bug, automaton, operation, schedule, ...)")
	edit.Flags().StringVar(&ask, "ask", "", "New list: the ask, word for word")
	edit.Flags().StringVar(&addAsk, "add-ask", "", "Later words added to the ask")
	edit.Flags().StringVar(&itemsFile, "items", "", "JSON file with an array of items to add or replace")
	edit.Flags().StringSliceVar(&remove, "remove", nil, "Item id to remove (repeatable)")
	edit.Flags().StringSliceVar(&packs, "pack", nil, "New list: optional default pack (repeatable)")
	edit.Flags().StringVar(&neededBy, "needed-by", "", "RFC 3339 time the result is needed by")
	edit.Flags().StringVar(&words, "words", "", "Why, or the words that were said")
	edit.Flags().StringVar(&hash, "hash", "", "The list hash you read")

	var fHash, fWords string
	freeze := &cobra.Command{Use: "freeze <request>", Short: "Fix the list you read, before work starts", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if fHash == "" {
				return fmt.Errorf("--hash is required: the hash shown by 'datawatch acceptance show'")
			}
			body := acceptanceCLIBody(args[0], fWords)
			body["freeze"], body["hash"] = true, fHash
			return daemonJSON(http.MethodPost, "/api/acceptance/edit", body)
		}}
	freeze.Flags().StringVar(&fHash, "hash", "", "The hash of the list you read (required)")
	freeze.Flags().StringVar(&fWords, "words", "", "The words that were said")

	var answer, cWords, relayed, cHash string
	confirm := &cobra.Command{Use: "confirm <request> <item|list>", Short: "Answer a confirm item, or confirm that the list covers the ask", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body := acceptanceCLIBody(args[0], cWords)
			body["item"], body["answer"] = args[1], answer
			setIfAny(body, "relayed_for", relayed)
			setIfAny(body, "hash", cHash)
			return daemonJSON(http.MethodPost, "/api/acceptance/confirm", body)
		}}
	confirm.Flags().StringVar(&answer, "answer", "", "yes or no (required)")
	confirm.Flags().StringVar(&cWords, "words", "", "The words that were said (required)")
	confirm.Flags().StringVar(&relayed, "relayed-for", "", "Who said it, when you pass on someone else's answer")
	confirm.Flags().StringVar(&cHash, "hash", "", "The list hash you read (required for item list)")

	var reason, aWords, aHash string
	accept := &cobra.Command{Use: "accept <request> <item>", Short: "Accept an item without proof (it never reads met)", Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body := acceptanceCLIBody(args[0], aWords)
			body["item"], body["reason"] = args[1], reason
			setIfAny(body, "hash", aHash)
			return daemonJSON(http.MethodPost, "/api/acceptance/accept", body)
		}}
	accept.Flags().StringVar(&reason, "reason", "", "Why it is accepted without proof (required)")
	accept.Flags().StringVar(&aWords, "words", "", "The words that were said (required)")
	accept.Flags().StringVar(&aHash, "hash", "", "The list hash you read")

	root.AddCommand(
		get("list", "Every list in short, with open items grouped by reason", "/api/acceptance"),
		show, edit, freeze, confirm, accept,
		get("report", "The standing check (report only; blocks nothing)", "/api/acceptance/report"),
		get("catalog", "Check methods, default items and project check names", "/api/acceptance/catalog"),
	)
	return root
}
