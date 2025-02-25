package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	esbuild "github.com/evanw/esbuild/pkg/api"
)

type WorkerRuntime struct {
	contexts map[string]esbuild.BuildContext
	results  map[string]esbuild.BuildResult
}

type WorkerProperties struct {
	AccountID  string         `json:"accountID"`
	ScriptName string         `json:"scriptName"`
	Build      NodeProperties `json:"build"`
}

func newWorkerRuntime() *WorkerRuntime {
	return &WorkerRuntime{
		contexts: map[string]esbuild.BuildContext{},
		results:  map[string]esbuild.BuildResult{},
	}
}

func (w *WorkerRuntime) Build(ctx context.Context, input *BuildInput) (*BuildOutput, error) {
	var properties WorkerProperties
	json.Unmarshal(input.Warp.Properties, &properties)
	build := properties.Build

	abs, err := filepath.Abs(input.Warp.Handler)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(input.Out(), input.Warp.Handler)

	slog.Info("loader info", "loader", build.Loader)

	loader := map[string]esbuild.Loader{}
	loaderMap := map[string]api.Loader{
		"js":      api.LoaderJS,
		"jsx":     api.LoaderJSX,
		"ts":      api.LoaderTS,
		"tsx":     api.LoaderTSX,
		"css":     api.LoaderCSS,
		"json":    api.LoaderJSON,
		"text":    api.LoaderText,
		"base64":  api.LoaderBase64,
		"file":    api.LoaderFile,
		"dataurl": api.LoaderDataURL,
		"binary":  api.LoaderBinary,
	}

	for key, value := range build.Loader {
		mapped, ok := loaderMap[value]
		if !ok {
			continue
		}
		loader[key] = mapped
	}

	options := esbuild.BuildOptions{
		Platform: esbuild.PlatformNode,
		Stdin: &esbuild.StdinOptions{
			Contents: fmt.Sprintf(`
      import handler from "%s"
      import { fromCloudflareEnv, wrapCloudflareHandler } from "sst"
      export default wrapCloudflareHandler(handler)
      `, abs),
			ResolveDir: filepath.Dir(abs),
			Loader:     esbuild.LoaderTS,
		},
		External:   []string{"node:*", "cloudflare:workers"},
		Conditions: []string{"worker"},
		Sourcemap:  esbuild.SourceMapNone,
		Loader:     loader,
		KeepNames:  true,
		Bundle:     true,
		Splitting:  build.Splitting,
		Metafile:   true,
		Write:      true,
		Plugins: []esbuild.Plugin{{
			Name: "node-prefix",
			Setup: func(build api.PluginBuild) {
				build.OnResolve(esbuild.OnResolveOptions{
					Filter: ".*",
				}, func(ora esbuild.OnResolveArgs) (esbuild.OnResolveResult, error) {
					if NODE_BUILTINS[ora.Path] {
						return esbuild.OnResolveResult{
							Path:     "node:" + ora.Path,
							External: true,
						}, nil
					}
					return esbuild.OnResolveResult{}, nil
				})
			},
		}},
		Outfile:           target,
		MinifyWhitespace:  build.Minify,
		MinifySyntax:      build.Minify,
		MinifyIdentifiers: build.Minify,
		Target:            esbuild.ESNext,
		Format:            esbuild.FormatESModule,
		MainFields:        []string{"module", "main"},
	}

	buildContext, ok := w.contexts[input.Warp.FunctionID]
	if !ok {
		buildContext, _ = esbuild.Context(options)
		w.contexts[input.Warp.FunctionID] = buildContext
	}

	result := buildContext.Rebuild()
	if len(result.Errors) == 0 {
		w.results[input.Warp.FunctionID] = result
	}
	errors := []string{}
	for _, error := range result.Errors {
		errors = append(errors, error.Text)
	}

	for _, error := range result.Errors {
		slog.Error("esbuild error", "error", error)
	}
	for _, warning := range result.Warnings {
		slog.Error("esbuild error", "error", warning)
	}

	return &BuildOutput{
		Handler: input.Warp.Handler,
		Errors:  errors,
	}, nil
}

func (w *WorkerRuntime) Match(runtime string) bool {
	return runtime == "worker"
}

func (w *WorkerRuntime) getFile(input *BuildInput) (string, bool) {
	dir := filepath.Dir(input.Warp.Handler)
	base := strings.Split(filepath.Base(input.Warp.Handler), ".")[0]
	for _, ext := range NODE_EXTENSIONS {
		file := filepath.Join(input.Project.PathRoot(), dir, base+ext)
		if _, err := os.Stat(file); err == nil {
			return file, true
		}
	}
	return "", false
}

func (r *WorkerRuntime) ShouldRebuild(functionID string, file string) bool {
	result, ok := r.results[functionID]
	if !ok {
		return false
	}

	var meta = map[string]interface{}{}
	err := json.Unmarshal([]byte(result.Metafile), &meta)
	if err != nil {
		return false
	}
	for key := range meta["inputs"].(map[string]interface{}) {
		absPath, err := filepath.Abs(key)
		if err != nil {
			continue
		}
		if absPath == file {
			return true
		}
	}

	return false
}

func (r *WorkerRuntime) Run(ctx context.Context, input *RunInput) (Worker, error) {
	return nil, fmt.Errorf("not implemented")
}

var NODE_BUILTINS = map[string]bool{
	"assert":              true,
	"async_hooks":         true,
	"buffer":              true,
	"child_process":       true,
	"cluster":             true,
	"console":             true,
	"constants":           true,
	"crypto":              true,
	"dgram":               true,
	"diagnostics_channel": true,
	"dns":                 true,
	"domain":              true,
	"events":              true,
	"fs":                  true,
	"http":                true,
	"http2":               true,
	"https":               true,
	"inspector":           true,
	"module":              true,
	"net":                 true,
	"os":                  true,
	"path":                true,
	"perf_hooks":          true,
	"process":             true,
	"punycode":            true,
	"querystring":         true,
	"readline":            true,
	"repl":                true,
	"stream":              true,
	"string_decoder":      true,
	"sys":                 true,
	"timers":              true,
	"tls":                 true,
	"trace_events":        true,
	"tty":                 true,
	"url":                 true,
	"util":                true,
	"v8":                  true,
	"vm":                  true,
	"wasi":                true,
	"worker_threads":      true,
	"zlib":                true,
}
