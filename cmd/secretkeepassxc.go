package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/coreos/go-semver/semver"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/twpayne/chezmoi/internal/chezmoi"
)

var keePassXCCmd = &cobra.Command{
	Use:     "keepassxc [args...]",
	Short:   "Execute the KeePassXC CLI (keepassxc-cli)",
	PreRunE: config.ensureNoError,
	RunE:    config.runKeePassXCCmd,
}

type keePassXCCmdConfig struct {
	Command  string
	Database string
	Args     []string
}

type keePassXCAttributeCacheKey struct {
	entry     string
	attribute string
}

var (
	keePassXCVersion                     *semver.Version
	keePassXCCache                       = make(map[string]map[string]string)
	keePassXCAttributeCache              = make(map[keePassXCAttributeCacheKey]string)
	keePassXCPairRegexp                  = regexp.MustCompile(`^([^:]+): (.*)$`)
	keePassXCPassword                    string
	keePassXCNeedShowProtectedArgVersion = semver.Version{Major: 2, Minor: 5, Patch: 1}
)

func init() {
	config.KeePassXC.Command = "keepassxc-cli"
	config.addTemplateFunc("keepassxc", config.keePassXCFunc)
	config.addTemplateFunc("keepassxcAttribute", config.keePassXCAttributeFunc)

	secretCmd.AddCommand(keePassXCCmd)
}

func (c *Config) runKeePassXCCmd(cmd *cobra.Command, args []string) error {
	return c.run("", c.KeePassXC.Command, args...)
}

func (c *Config) getKeePassXCVersion() *semver.Version {
	if keePassXCVersion != nil {
		return keePassXCVersion
	}
	name := c.KeePassXC.Command
	args := []string{"--version"}
	cmd := exec.Command(name, args...)
	output, err := c.mutator.IdempotentCmdOutput(cmd)
	if err != nil {
		panic(fmt.Errorf("%s %s: %w", name, chezmoi.ShellQuoteArgs(args), err))
	}
	keePassXCVersion, err = semver.NewVersion(string(bytes.TrimSpace(output)))
	if err != nil {
		panic(fmt.Errorf("cannot parse version %q: %w", output, err))
	}
	return keePassXCVersion
}

func (c *Config) keePassXCFunc(entry string) map[string]string {
	if data, ok := keePassXCCache[entry]; ok {
		return data
	}
	if c.KeePassXC.Database == "" {
		panic(errors.New("keepassxc.database not set"))
	}
	name := c.KeePassXC.Command
	args := []string{"show"}
	if c.getKeePassXCVersion().Compare(keePassXCNeedShowProtectedArgVersion) >= 0 {
		args = append(args, "--show-protected")
	}
	args = append(args, c.KeePassXC.Args...)
	args = append(args, c.KeePassXC.Database, entry)
	output, err := c.runKeePassXCCLICommand(name, args)
	if err != nil {
		panic(fmt.Errorf("%s %s: %w", name, chezmoi.ShellQuoteArgs(args), err))
	}
	data, err := parseKeyPassXCOutput(output)
	if err != nil {
		panic(fmt.Errorf("%s %s: %w", name, chezmoi.ShellQuoteArgs(args), err))
	}
	keePassXCCache[entry] = data
	return data
}

func (c *Config) keePassXCAttributeFunc(entry, attribute string) string {
	key := keePassXCAttributeCacheKey{
		entry:     entry,
		attribute: attribute,
	}
	if data, ok := keePassXCAttributeCache[key]; ok {
		return data
	}
	if c.KeePassXC.Database == "" {
		panic(errors.New("keepassxc.database not set"))
	}
	name := c.KeePassXC.Command
	args := []string{"show", "--attributes", attribute, "--quiet"}
	if c.getKeePassXCVersion().Compare(keePassXCNeedShowProtectedArgVersion) >= 0 {
		args = append(args, "--show-protected")
	}
	args = append(args, c.KeePassXC.Args...)
	args = append(args, c.KeePassXC.Database, entry)
	output, err := c.runKeePassXCCLICommand(name, args)
	if err != nil {
		panic(fmt.Errorf("%s %s: %w", name, chezmoi.ShellQuoteArgs(args), err))
	}
	outputStr := strings.TrimSpace(string(output))
	keePassXCAttributeCache[key] = outputStr
	return outputStr
}

func readPassword(prompt string) (pw []byte, err error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Print(prompt)
		pw, err = term.ReadPassword(fd)
		fmt.Println()
		return
	}

	var b [1]byte
	for {
		n, err := os.Stdin.Read(b[:])
		// term.ReadPassword discards any '\r', so do the same.
		if n > 0 && b[0] != '\r' {
			if b[0] == '\n' {
				return pw, nil
			}
			pw = append(pw, b[0])
			// limit size, so that a wrong input won't fill up the memory.
			if len(pw) > 1024 {
				err = errors.New("password too long")
			}
		}
		if err != nil {
			// term.ReadPassword accepts EOF-terminated passwords if non-empty,
			// so do the same.
			if errors.Is(err, io.EOF) && len(pw) > 0 {
				err = nil
			}
			return pw, err
		}
	}
}

func (c *Config) runKeePassXCCLICommand(name string, args []string) ([]byte, error) {
	if keePassXCPassword == "" {
		password, err := readPassword(fmt.Sprintf("Insert password to unlock %s: ", c.KeePassXC.Database))
		fmt.Println()
		if err != nil {
			return nil, err
		}
		keePassXCPassword = string(password)
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewBufferString(keePassXCPassword + "\n")
	cmd.Stderr = c.Stderr
	return c.mutator.IdempotentCmdOutput(cmd)
}

func parseKeyPassXCOutput(output []byte) (map[string]string, error) {
	data := make(map[string]string)
	s := bufio.NewScanner(bytes.NewReader(output))
	for i := 0; s.Scan(); i++ {
		if i == 0 {
			continue
		}
		match := keePassXCPairRegexp.FindStringSubmatch(s.Text())
		if match == nil {
			return nil, fmt.Errorf("cannot parse %q", s.Text())
		}
		data[match[1]] = match[2]
	}
	return data, s.Err()
}
