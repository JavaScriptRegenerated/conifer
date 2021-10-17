package main

import (
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/evanw/esbuild/pkg/api"
)

var httpPlugin = api.Plugin{
	Name: "http",
	Setup: func(build api.PluginBuild) {
		// Intercept import paths starting with "http:" and "https:" so
		// esbuild doesn't attempt to map them to a file system location.
		// Tag them with the "http-url" namespace to associate them with
		// this plugin.
		build.OnResolve(api.OnResolveOptions{Filter: `^https?://`},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				return api.OnResolveResult{
					Path:      args.Path,
					Namespace: "http-url",
				}, nil
			})

		// We also want to intercept all import paths inside downloaded
		// files and resolve them against the original URL. All of these
		// files will be in the "http-url" namespace. Make sure to keep
		// the newly resolved URL in the "http-url" namespace so imports
		// inside it will also be resolved as URLs recursively.
		build.OnResolve(api.OnResolveOptions{Filter: ".*", Namespace: "http-url"},
			func(args api.OnResolveArgs) (api.OnResolveResult, error) {
				base, err := url.Parse(args.Importer)
				if err != nil {
					return api.OnResolveResult{}, err
				}
				relative, err := url.Parse(args.Path)
				if err != nil {
					return api.OnResolveResult{}, err
				}
				return api.OnResolveResult{
					Path:      base.ResolveReference(relative).String(),
					Namespace: "http-url",
				}, nil
			})

		// When a URL is loaded, we want to actually download the content
		// from the internet. This has just enough logic to be able to
		// handle the example import from unpkg.com but in reality this
		// would probably need to be more complex.
		build.OnLoad(api.OnLoadOptions{Filter: ".*", Namespace: "http-url"},
			func(args api.OnLoadArgs) (api.OnLoadResult, error) {
				res, err := http.Get(args.Path)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				defer res.Body.Close()
				bytes, err := io.ReadAll(res.Body)
				if err != nil {
					return api.OnLoadResult{}, err
				}
				contents := string(bytes)
				return api.OnLoadResult{Contents: &contents}, nil
			})
	},
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// region := os.Getenv("FLY_REGION")

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var source = ""
		if r.URL.Path == "/health" {
			source = `
			// export * from './another-file'
			// import * as constants from 'https://raw.githubusercontent.com/RoyalIcing/modules/main/constants.js'
			// export { constants };
			export const hello = 'world';
			export * from 'https://raw.githubusercontent.com/RoyalIcing/modules/0003a973c63dfc78bbc595d5d3b7891b89a1b829/constants.js'
			export * from 'https://raw.githubusercontent.com/RoyalIcing/modules/0003a973c63dfc78bbc595d5d3b7891b89a1b829/interpolation.js'
			export * from 'https://raw.githubusercontent.com/RoyalIcing/modules/0003a973c63dfc78bbc595d5d3b7891b89a1b829/generators.js'
			// export const pi = Math.PI;
			`
		} else if r.Method == "POST" {
			defer r.Body.Close()
			if b, err := io.ReadAll(r.Body); err == nil {
				source = string(b)
			}
		} else if r.URL.Path == "/react@17.0.2" {
			source = `
			export * from "https://cdn.jsdelivr.net/npm/react@17.0.2/umd/react.production.min.js";
			//export * from "https://cdn.jsdelivr.net/npm/react-dom@17.0.2/umd/react-dom.production.min.js";
			`
		} else {
			source = r.URL.Query().Get("source")
		}

		var minify = r.URL.Query().Has("minify")

		result := api.Build(api.BuildOptions{
			Stdin: &api.StdinOptions{
				Contents: source,
				// These are all optional:
				ResolveDir: "./src",
				Sourcefile: "imaginary-file.js",
				Loader:     api.LoaderJS,
			},
			Format:            api.FormatESModule,
			Bundle:            true,
			Plugins:           []api.Plugin{httpPlugin},
			Write:             false,
			MinifyWhitespace:  minify,
			MinifyIdentifiers: minify,
			MinifySyntax:      minify,
		})

		if len(result.Errors) > 0 {
			http.Error(w, result.Errors[0].Text, http.StatusInternalServerError)
			return
		}

		w.Header().Add("Content-Type", "text/javascript;charset=UTF-8")
		w.WriteHeader(http.StatusOK)

		if len(result.OutputFiles) > 0 {
			w.Write(result.OutputFiles[0].Contents)
		}
	})

	log.Println("listening on", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
