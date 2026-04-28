// Command holos-console-migrate-default-folder is a one-shot operator-run
// migration tool that strips the removed
// `console.holos.run/default-folder` annotation from existing organization
// namespaces.
//
// The tool runs in dry-run mode by default; pass --apply to perform writes. It
// is idempotent: re-running it after a successful migration is a no-op once no
// organization namespace carries the annotation.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"text/tabwriter"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
)

const defaultFolderAnnotation = "console.holos.run/default-folder"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type options struct {
	apply      bool
	kubeconfig string
}

func parseFlags(args []string, errOut io.Writer) (*options, error) {
	fs := flag.NewFlagSet("holos-console-migrate-default-folder", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, "Usage: %s [flags]\n\n", fs.Name())
		fmt.Fprintln(errOut, "One-shot migration: strip the removed default-folder annotation from organization namespaces.")
		fmt.Fprintln(errOut, "Runs as the operator-supplied kubeconfig identity.")
		fmt.Fprintln(errOut)
		fmt.Fprintln(errOut, "Flags:")
		fs.PrintDefaults()
	}
	opts := &options{}
	fs.BoolVar(&opts.apply, "apply", false, "Perform writes. Without --apply the tool only reports planned changes.")
	fs.StringVar(&opts.kubeconfig, "kubeconfig", "", "Path to kubeconfig (defaults to KUBECONFIG env, then ~/.kube/config, then in-cluster config)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return opts, nil
}

func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	client, err := buildClient(opts.kubeconfig)
	if err != nil {
		return fmt.Errorf("building kube client: %w", err)
	}
	ctx := context.Background()
	report, err := Migrate(ctx, client, opts.apply)
	if err != nil {
		return err
	}
	return PrintReport(stdout, report, opts.apply)
}

func buildClient(kubeconfig string) (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	)
	cfg, err := loader.ClientConfig()
	if err != nil {
		if kubeconfig != "" {
			return nil, err
		}
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(cfg)
}

type NamespaceReport struct {
	Namespace             string
	DefaultFolderFound    bool
	DefaultFolderStripped bool
}

type Report struct {
	Namespaces []NamespaceReport
}

// Migrate walks every console-managed organization namespace and strips the
// removed default-folder annotation when apply is true. In apply mode it
// verifies the invariant before returning: no organization namespace may still
// carry the annotation.
func Migrate(ctx context.Context, client kubernetes.Interface, apply bool) (*Report, error) {
	nsList, err := listOrganizationNamespaces(ctx, client)
	if err != nil {
		return nil, err
	}
	report := &Report{}
	for i := range nsList.Items {
		ns := &nsList.Items[i]
		nr := NamespaceReport{Namespace: ns.Name}
		_, found := ns.Annotations[defaultFolderAnnotation]
		nr.DefaultFolderFound = found
		if found {
			if apply {
				if err := stripDefaultFolderAnnotation(ctx, client, ns); err != nil {
					return nil, fmt.Errorf("stripping %s from namespace %q: %w", defaultFolderAnnotation, ns.Name, err)
				}
			}
			nr.DefaultFolderStripped = true
		}
		report.Namespaces = append(report.Namespaces, nr)
	}
	if apply {
		if err := assertNoDefaultFolderAnnotations(ctx, client); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func listOrganizationNamespaces(ctx context.Context, client kubernetes.Interface) (*corev1.NamespaceList, error) {
	selector := labels.SelectorFromSet(labels.Set{
		v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
		v1alpha2.LabelResourceType: v1alpha2.ResourceTypeOrganization,
	})
	nsList, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing organization namespaces: %w", err)
	}
	sort.Slice(nsList.Items, func(i, j int) bool {
		return nsList.Items[i].Name < nsList.Items[j].Name
	})
	return nsList, nil
}

func stripDefaultFolderAnnotation(ctx context.Context, client kubernetes.Interface, ns *corev1.Namespace) error {
	live, err := client.CoreV1().Namespaces().Get(ctx, ns.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if live.Annotations == nil {
		return nil
	}
	if _, ok := live.Annotations[defaultFolderAnnotation]; !ok {
		return nil
	}
	delete(live.Annotations, defaultFolderAnnotation)
	_, err = client.CoreV1().Namespaces().Update(ctx, live, metav1.UpdateOptions{})
	return err
}

func assertNoDefaultFolderAnnotations(ctx context.Context, client kubernetes.Interface) error {
	nsList, err := listOrganizationNamespaces(ctx, client)
	if err != nil {
		return err
	}
	var remaining []string
	for i := range nsList.Items {
		ns := &nsList.Items[i]
		if _, ok := ns.Annotations[defaultFolderAnnotation]; ok {
			remaining = append(remaining, ns.Name)
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("%s still present on organization namespaces: %v", defaultFolderAnnotation, remaining)
	}
	return nil
}

func PrintReport(w io.Writer, report *Report, applied bool) error {
	if report == nil {
		return nil
	}
	mode := "DRY-RUN"
	if applied {
		mode = "APPLIED"
	}
	if _, err := fmt.Fprintf(w, "holos-console-migrate-default-folder (%s)\n", mode); err != nil {
		return err
	}
	if len(report.Namespaces) == 0 {
		_, err := fmt.Fprintln(w, "no organization namespaces found.")
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tDEFAULT-FOLDER-FOUND\tDEFAULT-FOLDER-STRIPPED")
	for _, nr := range report.Namespaces {
		fmt.Fprintf(tw, "%s\t%t\t%t\n",
			nr.Namespace,
			nr.DefaultFolderFound,
			nr.DefaultFolderStripped,
		)
	}
	return tw.Flush()
}
