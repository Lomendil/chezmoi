package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/twpayne/chezmoi/next/internal/chezmoi"
)

type dumpCmdConfig struct {
	include   *chezmoi.IncludeSet
	recursive bool
}

func (c *Config) newDumpCmd() *cobra.Command {
	dumpCmd := &cobra.Command{
		Use:     "dump [target]...",
		Short:   "Generate a dump of the target state",
		Long:    mustLongHelp("dump"),
		Example: example("dump"),
		RunE:    c.runDumpCmd,
	}

	flags := dumpCmd.Flags()
	flags.VarP(c.dump.include, "include", "i", "include entry types")
	flags.BoolVarP(&c.dump.recursive, "recursive", "r", c.dump.recursive, "recursive")

	return dumpCmd
}

func (c *Config) runDumpCmd(cmd *cobra.Command, args []string) error {
	dumpSystem := chezmoi.NewDumpSystem()
	if err := c.applyArgs(dumpSystem, "", args, c.dump.include, c.dump.recursive, os.ModePerm); err != nil {
		return err
	}
	return c.marshal(dumpSystem.Data())
}