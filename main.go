package main

import (
	"io/ioutil"
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
				bytes, err := ioutil.ReadAll(res.Body)
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
		result := api.Build(api.BuildOptions{
			Stdin: &api.StdinOptions{
				Contents: `
				// export * from './another-file'
				// import * as constants from 'https://raw.githubusercontent.com/RoyalIcing/modules/main/constants.js'
				// export { constants };
				export * from 'https://raw.githubusercontent.com/RoyalIcing/modules/main/constants.js'
				// export const pi = Math.PI;
				`,

				// These are all optional:
				ResolveDir: "./src",
				Sourcefile: "imaginary-file.js",
				Loader:     api.LoaderJS,
			},
			Format: api.FormatESModule,

			// EntryPoints: []string{"app.js"},
			Bundle: true,
			// Outfile:     "out.js",
			Plugins: []api.Plugin{httpPlugin},
			Write:   false,
		})

		if len(result.Errors) > 0 {
			w.Header().Add("Content-Type", "text/plain;charset=UTF-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(result.Errors[0].Text))
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
