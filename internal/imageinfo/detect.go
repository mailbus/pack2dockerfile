package imageinfo

import (
	"sort"
	"strings"
)

// languagePrefixes maps a language family to the Paketo buildpack ID
// prefixes that indicate it. Utility buildpacks (ca-certificates, procfile,
// syft, ...) intentionally match nothing.
var languagePrefixes = map[string][]string{
	"node": {
		"paketo-buildpacks/nodejs",
		"paketo-buildpacks/node-engine",
		"paketo-buildpacks/node-start",
		"paketo-buildpacks/npm-",
		"paketo-buildpacks/yarn",
		"paketo-buildpacks/pnpm",
	},
	"python": {
		"paketo-buildpacks/python",
		"paketo-buildpacks/cpython",
		"paketo-buildpacks/pip",
		"paketo-buildpacks/pipenv",
		"paketo-buildpacks/poetry",
		"paketo-buildpacks/miniconda",
	},
	"go": {
		"paketo-buildpacks/go",
		"paketo-buildpacks/go-dist",
		"paketo-buildpacks/go-build",
	},
	"java": {
		"paketo-buildpacks/java",
		"paketo-buildpacks/bellsoft-liberica",
		"paketo-buildpacks/executable-jar",
		"paketo-buildpacks/jvm-application",
		"paketo-buildpacks/maven",
		"paketo-buildpacks/gradle",
		"paketo-buildpacks/tomcat",
		"paketo-buildpacks/spring-boot",
	},
}

// detectLanguages returns the sorted language families found in buildpack IDs.
func detectLanguages(buildpackIDs []string) []string {
	found := map[string]bool{}
	for _, id := range buildpackIDs {
		for lang, prefixes := range languagePrefixes {
			for _, p := range prefixes {
				if strings.HasPrefix(id, p) {
					found[lang] = true
					break
				}
			}
		}
	}
	langs := make([]string, 0, len(found))
	for l := range found {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return langs
}
