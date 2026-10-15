// Copyright (C) ConfigHub, Inc.
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/skratchdot/open-golang/open"
	"github.com/spf13/cobra"
)

var clusterOpenArgs struct {
	name          string
	printPassword bool
}

var clusterOpenCmd = &cobra.Command{
	Use:   "open [<cluster-name>]",
	Short: "Open a cluster's Argo CD UI or Sveltos dashboard in the browser",
	Args:  cobra.MaximumNArgs(1),
	Long: getCommandHelp(`Open the Argo CD admin UI of a cub-managed cluster, log-in credentials in hand:
the admin password is copied to your clipboard so you can paste it into Argo's
login form instead of digging it out of the cluster.

For a cluster from "cub cluster up --controller sveltos", this opens the Sveltos
dashboard instead, and copies a login token to your clipboard. The token is
created each time from the cluster's platform-admin ServiceAccount, and is valid
for 24 hours. --print-password prints it instead.

A cluster from "cub cluster up --controller flux" has no UI to open, so this
prints the commands that show Flux's state instead.

The cluster name is given as an argument, and can be omitted
when this host has exactly one cub-managed cluster. `+"`cub cluster list`"+` shows the
names and Argo endpoints of all of them.

The endpoint and password come from the cluster's env file
(<config-dir>/clusters/<name>.env, written by `+"`cub cluster up`"+`); if that file is
gone, the endpoint is read from the cluster's ConfigHub Space annotations, in
the context the cluster's record says it is wired to, and the password from the
argocd-initial-admin-secret in the live cluster. An attached cluster has no
host port for Argo CD; this prints the port-forward to use instead. If no password
can be found — an admin who changes it deletes that Secret — the UI is still
opened and only the username is reported.

Examples:
`+"```"+`
  # Open the Argo CD UI of the only cluster on this host
  cub cluster open

  # Open a specific cluster's Argo CD UI
  cub cluster open my-cluster

  # Print the admin password instead of copying it to the clipboard
  cub cluster open my-cluster --print-password

  # Print the Argo CD URL instead of opening a browser
  cub cluster open my-cluster --print-url
`+"```"+`
`, ""),
	RunE: clusterOpenCmdRun,
}

func init() {
	enableOpenFlags(clusterOpenCmd)
	clusterOpenCmd.Flags().StringVar(&clusterOpenArgs.name, "name", "", "cluster name (the argument; kept as a flag for older scripts)")
	_ = clusterOpenCmd.Flags().MarkHidden("name")
	clusterOpenCmd.Flags().BoolVar(&clusterOpenArgs.printPassword, "print-password", false, "print the admin password (for Sveltos, the dashboard token) to the terminal instead of copying it to the clipboard")
	clusterCmd.AddCommand(clusterOpenCmd)
}

func clusterOpenCmdRun(cmd *cobra.Command, args []string) error {
	name, err := clusterNameArg(args, clusterOpenArgs.name, "open", false)
	if err != nil {
		return err
	}
	return clusterOpenRun(cmd.OutOrStdout(), name, clusterOpenArgs.printPassword)
}

func clusterOpenRun(out io.Writer, name string, printPassword bool) error {
	name, err := clusterResolveName(name)
	if err != nil {
		return err
	}

	// The env file is the local, no-round-trip source for both the endpoint and
	// the password; each has its own fallback below when it is absent. The
	// fallbacks ask the cluster's Space, so act in the context it is wired to.
	env := clusterReadEnvFile(name)
	if rec, err := clusterReadRecord(name); err != nil {
		return err
	} else if env["ARGOCD_SERVER"] == "" && env["SVELTOS_DASHBOARD"] == "" {
		if rec != nil && rec.Origin == clusterOriginAttach {
			return fmt.Errorf("cluster %q was attached, and attach does not expose Argo CD on a host port; reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n %s 8080:80\nand open http://localhost:8080",
				name, clusterKubeconfigPath(name), clusterArgoNamespace)
		}
		if err := clusterUseRecordedContext(out, rec); err != nil {
			return err
		}
	}

	controller, space, err := clusterOpenController(name, env)
	if err != nil {
		return err
	}
	switch controller {
	case clusterControllerSveltos:
		return clusterOpenSveltos(out, name, env, space, printPassword)
	case clusterControllerFlux:
		return clusterOpenFlux(out, name)
	}

	url, err := clusterArgoURL(name, env)
	if err != nil {
		return err
	}

	if printURL {
		fmt.Fprintln(out, url)
		return nil
	}

	// Fetch the password before launching the browser, so the paste is ready by
	// the time the login form is.
	password, passwordErr := clusterArgoPassword(name, env)

	if err := open.Start(url); err != nil {
		return fmt.Errorf("failed to open browser on %s: %w", url, err)
	}
	fmt.Fprintf(out, "Opened the Argo CD UI of cluster %q: %s\n", name, url)
	fmt.Fprintf(out, "  user:     admin\n")
	switch {
	case password == "":
		fmt.Fprintf(out, "  password: could not be determined (%v)\n", passwordErr)
	case printPassword:
		fmt.Fprintf(out, "  password: %s\n", password)
	default:
		if cerr := clipboardCopy(ctx, password); cerr != nil {
			fmt.Fprintf(out, "  password: %s  (could not copy to clipboard: %v)\n", password, cerr)
		} else {
			fmt.Fprintf(out, "  password: in your clipboard — paste it into Argo's login form\n")
		}
	}
	return nil
}

// clusterOpenController returns the controller the cluster runs. The env file
// answers without a round-trip: `cub cluster up` writes SVELTOS_DASHBOARD for a
// Sveltos cluster and ARGOCD_SERVER for an Argo one. Without either, as for a
// Flux cluster, the cluster Space's controller annotation decides, and the
// Space is returned for the caller's own fallbacks. A cluster with no Space is
// taken to be an Argo one.
func clusterOpenController(name string, env map[string]string) (string, *clusterSpace, error) {
	switch {
	case env["SVELTOS_DASHBOARD"] != "":
		return clusterControllerSveltos, nil, nil
	case env["ARGOCD_SERVER"] != "":
		return clusterControllerArgo, nil, nil
	}
	space, err := clusterSpaceByName(name)
	if err != nil {
		return "", nil, err
	}
	if space == nil {
		return clusterControllerArgo, nil, nil
	}
	return space.Controller, space, nil
}

// clusterOpenFlux tells how to look at a Flux cluster, which has no UI to open.
func clusterOpenFlux(out io.Writer, name string) error {
	if printURL {
		return fmt.Errorf("cluster %q runs Flux, which has no UI", name)
	}
	fmt.Fprintf(out, "Cluster %q runs Flux, which has no UI. See its state with:\n", name)
	fmt.Fprintf(out, "  source %s\n", clusterEnvFilePath(name))
	if _, err := exec.LookPath("flux"); err == nil {
		fmt.Fprintf(out, "  flux get all -A\n")
		return nil
	}
	fmt.Fprintf(out, "  %s\n", clusterFluxStatusCommand())
	return nil
}

// clusterOpenSveltos opens a Sveltos cluster's dashboard with a fresh login
// token in hand: copied to the clipboard, or printed with --print-password.
// Without a token the dashboard is still opened, and the error is reported.
func clusterOpenSveltos(out io.Writer, name string, env map[string]string, space *clusterSpace, printToken bool) error {
	url, err := clusterSveltosDashboardEndpoint(name, env, space)
	if err != nil {
		return err
	}
	if printURL {
		fmt.Fprintln(out, url)
		return nil
	}

	// Create the token before launching the browser, so the paste is ready by
	// the time the login form is.
	token, tokenErr := clusterSveltosDashboardToken(ctx, name)

	if err := open.Start(url); err != nil {
		return fmt.Errorf("failed to open browser on %s: %w", url, err)
	}
	fmt.Fprintf(out, "Opened the Sveltos dashboard of cluster %q: %s\n", name, url)
	switch {
	case token == "":
		fmt.Fprintf(out, "  token: could not be created (%v)\n", tokenErr)
	case printToken:
		fmt.Fprintf(out, "  token: %s\n", token)
	default:
		if cerr := clipboardCopy(ctx, token); cerr != nil {
			fmt.Fprintf(out, "  token: %s  (could not copy to clipboard: %v)\n", token, cerr)
		} else {
			fmt.Fprintf(out, "  token: in your clipboard, valid for %s — paste it into the dashboard's login form\n", clusterSveltosDashboardTokenDuration)
		}
	}
	return nil
}

// clusterResolveName passes an explicitly named cluster through, or picks the
// only cub-managed cluster on this host when the name was omitted. "cub-managed
// on this host" means a kubeconfig at <config-dir>/clusters/<name>.kubeconfig,
// the same marker `cub cluster list` reads — so the common case resolves without
// a server round-trip.
func clusterResolveName(name string) (string, error) {
	if name != "" {
		return name, nil
	}
	local, err := clusterLocalNames()
	if err != nil {
		return "", err
	}
	switch len(local) {
	case 0:
		return "", fmt.Errorf("no cub-managed clusters on this host; create one with 'cub cluster up'")
	case 1:
		return local[0], nil
	default:
		return "", fmt.Errorf("%d cub-managed clusters on this host (%s); name the one to open: 'cub cluster open <cluster-name>'",
			len(local), strings.Join(local, ", "))
	}
}

// clusterArgoURL resolves the cluster's Argo CD endpoint, preferring the env
// file's ARGOCD_SERVER and falling back to the argo-port annotation on the
// cluster's Space in the current context.
func clusterArgoURL(name string, env map[string]string) (string, error) {
	if server := env["ARGOCD_SERVER"]; server != "" {
		if strings.Contains(server, "://") {
			return server, nil
		}
		// Argo CD is installed with server.insecure=true and exposed on a
		// NodePort, so the endpoint is plain HTTP.
		return "http://" + server, nil
	}

	space, err := clusterSpaceByName(name)
	if err != nil {
		return "", fmt.Errorf("cluster %q: no ARGOCD_SERVER in %s, so its Argo CD port has to come from its ConfigHub space: %w",
			name, clusterEnvFilePath(name), err)
	}
	if space == nil {
		return "", fmt.Errorf("cluster %q: no env file at %s and no cub-managed Space for it in the current cub context (are you in the right context? see 'cub cluster list')",
			name, clusterEnvFilePath(name))
	}
	if space.Origin == clusterOriginAttach {
		return "", fmt.Errorf("cluster %q was attached, and attach does not expose Argo CD on a host port; reach the UI with\n  kubectl --kubeconfig %s port-forward svc/argocd-server -n %s 8080:80\nand open http://localhost:8080",
			name, clusterKubeconfigPath(name), clusterArgoNamespace)
	}
	if space.ArgoPort == "" {
		return "", fmt.Errorf("cluster %q: space %q has no %s annotation, so its Argo CD port is unknown",
			name, space.SpaceSlug, clusterAnnotationArgoPort)
	}
	return "http://localhost:" + space.ArgoPort, nil
}

// clusterArgoPassword resolves the Argo CD admin password, preferring the env
// file and falling back to the argocd-initial-admin-secret in the live cluster.
// A cluster whose admin has rotated the password has neither (Argo's own docs
// tell you to delete that Secret afterwards), so an empty password with a
// non-nil error is an expected outcome the caller reports rather than fails on.
func clusterArgoPassword(name string, env map[string]string) (string, error) {
	if password := env["ARGOCD_PASSWORD"]; password != "" {
		return password, nil
	}
	kubeconfigPath := clusterKubeconfigPath(name)
	if _, err := os.Stat(kubeconfigPath); err != nil {
		return "", fmt.Errorf("no ARGOCD_PASSWORD in %s and no kubeconfig at %s", clusterEnvFilePath(name), kubeconfigPath)
	}
	password, err := clusterArgoAdminPassword(ctx, kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("no ARGOCD_PASSWORD in %s and could not read it from the cluster: %w", clusterEnvFilePath(name), err)
	}
	return password, nil
}

// clusterSpaceByName returns the cluster Space for a cluster name, or nil if
// the active cub context has none. A cluster with a record is looked up in its
// organization whichever machine created it; one without is matched on this
// host's annotation, as before records existed.
func clusterSpaceByName(name string) (*clusterSpace, error) {
	rec, err := clusterReadRecord(name)
	if err != nil {
		return nil, err
	}
	if rec != nil {
		return clusterSpaceByNameInOrg(name)
	}
	hostname, _ := os.Hostname()
	spaces, err := clusterListSpacesForHost(hostname)
	if err != nil {
		return nil, fmt.Errorf("look up cluster spaces: %w", err)
	}
	for i := range spaces {
		if spaces[i].ClusterName == name {
			return &spaces[i], nil
		}
	}
	return nil, nil
}

// clusterReadEnvFile parses the cluster's env file, returning an empty map if it
// is unreadable — every value has a fallback, so a missing file is not an error
// here.
func clusterReadEnvFile(name string) map[string]string {
	contents, err := os.ReadFile(clusterEnvFilePath(name))
	if err != nil {
		return map[string]string{}
	}
	return clusterParseEnvFile(string(contents))
}

// clusterParseEnvFile parses the `export KEY=VALUE` lines clusterBuildEnvFile
// writes, unquoting Go-quoted values (the password is written with %q).
func clusterParseEnvFile(contents string) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		env[strings.TrimSpace(key)] = value
	}
	return env
}
