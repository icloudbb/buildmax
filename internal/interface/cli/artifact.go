package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/interface/client"
	"github.com/icloudbb/buildmax/internal/tool"
)

func newArtifactCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Publish artifacts to the server",
		Long: "Upload files to the BuildMax server you are signed in to, so a run's\n" +
			"output has a durable handle instead of living only in a workspace.\n" +
			"Sign in with `buildmax login` first.",
	}
	cmd.AddCommand(newArtifactPublishCommand())
	return cmd
}

func newArtifactPublishCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <file>",
		Short: "Upload a local file as an artifact and print its id",
		Long: "Uploads a file and prints the artifact id. That id is the exact handle\n" +
			"to name in an issue comment's `Artifacts:` line, so an agent can produce\n" +
			"a result here and point the thread at it.\n\n" +
			"Without --space the artifact goes to your personal space. --share also\n" +
			"mints a public link.",
		Args: cobra.ExactArgs(1),
		RunE: runArtifactPublish,
	}
	cmd.Flags().String("space", "", "the space to store it in; your personal space when omitted")
	cmd.Flags().String("title", "", "a title for the artifact")
	cmd.Flags().Bool("share", false, "also mint a public share link")
	return cmd
}

func runArtifactPublish(cmd *cobra.Command, args []string) error {
	path := args[0]
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; publish a file", path)
	}
	title, _ := cmd.Flags().GetString("title")
	share, _ := cmd.Flags().GetBool("share")
	out := cmd.OutOrStdout()

	// Inside a worker run the space is the run's, derived from the run token, so
	// --space does not apply; the upload goes through the bridge to the worker
	// route.
	if wb := inWorkerRun(); wb != nil {
		pub := workerclient.NewArtifactPublisher(wb.cfg, wb.taskRunID, "")
		art, err := pub.PublishArtifact(cmd.Context(), tool.ArtifactUpload{
			Path: path, Filename: filepath.Base(path), Title: title, Share: share,
		})
		if err != nil {
			return fmt.Errorf("publish artifact: %w", err)
		}
		fmt.Fprintf(out, "Published %s (%d bytes).\n", art.Filename, art.SizeBytes)
		fmt.Fprintf(out, "artifact %s\n", art.ArtifactID)
		if art.ShareURL != "" {
			fmt.Fprintf(out, "share %s\n", art.ShareURL)
		}
		return nil
	}

	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	space, _ := cmd.Flags().GetString("space")
	art, err := client.NewClient(serverURL).PublishArtifact(
		cmd.Context(), token, space, title, path, filepath.Base(path), share)
	if err != nil {
		return fmt.Errorf("publish artifact: %w", err)
	}
	fmt.Fprintf(out, "Published %s (%d bytes).\n", art.Filename, art.SizeBytes)
	fmt.Fprintf(out, "artifact %s\n", art.ID)
	if art.ShareURL != "" {
		fmt.Fprintf(out, "share %s\n", art.ShareURL)
	}
	if art.ShareError != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: share link not created: %s\n", art.ShareError)
	}
	return nil
}
