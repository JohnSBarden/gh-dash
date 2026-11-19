// Package slotcmd implements `gh dash slot <N>`: acting on a numbered inbox
// row from outside the panel, using the slots.json the panel exports.
package slotcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/handoff"
	"github.com/dlvhdr/gh-dash/v4/internal/inbox"
)

// Actions of the slot command.
const (
	ActionAgent = "agent"
	ActionDone  = "done"
	ActionOpen  = "open"
)

// staleAfter is how old slots.json may be before the command warns that the
// panel is probably not running.
const staleAfter = 2 * time.Hour

// ErrCancelled is returned when the user backs out at a prompt.
var ErrCancelled = errors.New("cancelled")

type Options struct {
	Slot   int
	Action string
	// Prompt skips the interactive question of the agent action.
	Prompt string
	// Yes skips the confirmation of the done action.
	Yes bool
}

// Deps are the side effects of the command, injected for tests.
type Deps struct {
	Store inbox.Store
	In    io.Reader
	Out   io.Writer
	// Interactive is true when a person can answer questions on In.
	Interactive bool
	Agent       config.AgentConfig
	Env         handoff.Env
	Runner      handoff.Runner
	MarkDone    func(threadID string) error
	Open        func(url string) error
	Now         func() time.Time
}

// Run performs the action on the item in the slot. It always prints the item
// first, so acting on a list that changed since it was last looked at is
// never silent.
func Run(opts Options, deps Deps) error {
	doc, err := deps.Store.ReadSlots()
	if err != nil {
		return err
	}
	item, err := doc.Find(opts.Slot)
	if err != nil {
		return err
	}
	describe(deps.Out, opts.Slot, item, doc, deps.Now())

	switch opts.Action {
	case ActionOpen:
		if item.URL == "" {
			return errors.New("this item has no URL")
		}
		if err := deps.Open(item.URL); err != nil {
			return err
		}
		fmt.Fprintln(deps.Out, "Opened in the browser")
		return nil
	case ActionDone:
		return done(opts, deps, item)
	case ActionAgent:
		return agent(opts, deps, item)
	default:
		return fmt.Errorf("unknown action %q", opts.Action)
	}
}

func describe(out io.Writer, slot int, item inbox.Item, doc inbox.SlotsDoc, now time.Time) {
	ref := item.Repo
	if item.Number > 0 {
		ref = fmt.Sprintf("%s #%d", item.Repo, item.Number)
	}
	fmt.Fprintf(out, "Slot %d  %s\n%s\n%s\n", slot, ref, item.Title, item.URL)
	if age := now.Sub(doc.GeneratedAt); age > staleAfter {
		fmt.Fprintf(
			out,
			"Warning: the slots were last written %s ago, the panel may not be running\n",
			age.Round(time.Minute),
		)
	}
	fmt.Fprintln(out)
}

func readLine(in io.Reader) string {
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line)
}

func done(opts Options, deps Deps, item inbox.Item) error {
	if !opts.Yes && deps.Interactive {
		fmt.Fprint(deps.Out, "Mark as done? [Y/n] ")
		if answer := strings.ToLower(readLine(deps.In)); answer != "" && answer != "y" && answer != "yes" {
			return ErrCancelled
		}
	}
	if err := deps.Store.Done(item); err != nil {
		return err
	}
	if !item.HasNotification() {
		fmt.Fprintln(deps.Out, "Hidden until it is updated")
		return nil
	}
	if err := deps.MarkDone(item.ThreadID); err != nil {
		return fmt.Errorf("hidden locally, but marking the notification done failed: %w", err)
	}
	fmt.Fprintln(deps.Out, "Notification marked as done")
	return nil
}

func agent(opts Options, deps Deps, item inbox.Item) error {
	prompt := strings.TrimSpace(opts.Prompt)
	if prompt == "" {
		if !deps.Interactive {
			return errors.New("no prompt: pass --prompt or run in a terminal")
		}
		fmt.Fprint(deps.Out, "Prompt (empty cancels): ")
		if prompt = readLine(deps.In); prompt == "" {
			return ErrCancelled
		}
	}

	suffix := strconv.FormatInt(deps.Now().Unix()%(36*36*36), 36)
	summary, err := handoff.Send(deps.Agent, deps.Env, handoff.Context{
		Prompt:  prompt,
		Title:   item.Title,
		URL:     item.URL,
		Repo:    item.Repo,
		Number:  item.Number,
		Type:    item.Type,
		Reasons: item.Reasons,
	}, suffix, deps.Runner)
	if err != nil {
		return err
	}
	fmt.Fprintln(deps.Out, summary)
	return nil
}
