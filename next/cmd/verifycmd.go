package cmd

import (
	"github.com/spf13/cobra"

	"github.com/twpayne/chezmoi/next/internal/chezmoi"
)

type verifyCmdConfig struct {
	include   *chezmoi.IncludeSet
	recursive bool
}

func (c *Config) newVerifyCmd() *cobra.Command {
	verifyCmd := &cobra.Command{
		Use:     "verify [target]...",
		Short:   "Exit with success if the destination state matches the target state, fail otherwise",
		Long:    mustLongHelp("verify"),
		Example: example("verify"),
		RunE:    c.runVerifyCmd,
	}

	flags := verifyCmd.Flags()
	flags.VarP(c.verify.include, "include", "i", "include entry types")
	flags.BoolVarP(&c.verify.recursive, "recursive", "r", c.verify.recursive, "recursive")

	return verifyCmd
}

func (c *Config) runVerifyCmd(cmd *cobra.Command, args []string) error {
	dryRunSystem := chezmoi.NewDryRunSystem(c.destSystem)
	if err := c.applyArgs(dryRunSystem, c.absSlashDestDir, args, c.verify.include, c.verify.recursive, c.Umask.FileMode()); err != nil {
		return err
	}
	if dryRunSystem.Modified() {
		return ErrExitCode(1)
	}
	return nil
}