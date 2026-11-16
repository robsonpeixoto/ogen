package gen

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-faster/errors"

	"github.com/ogen-go/ogen/gen/ir"
	"github.com/ogen-go/ogen/location"
)

// checkExternalMux rejects operations that Server.Routes cannot expose
// as http.ServeMux-compatible routes.
func (g *Generator) checkExternalMux() error {
	if !g.features.Has(PathsServer) || !g.features.Has(ServerExternalMux) {
		return nil
	}
	for _, op := range g.operations {
		if err := checkExternalMuxOperation(op); err != nil {
			return wrapOperationLocation(op, errors.Wrapf(err, "%s %s",
				strings.ToUpper(op.Spec.HTTPMethod),
				op.Spec.Path.String(),
			))
		}
	}
	return checkExternalMuxConflicts(g.operations)
}

func checkExternalMuxOperation(op *ir.Operation) error {
	if op.Spec.OperationID == "" {
		return errors.Errorf("feature %q requires operationId", ServerExternalMux.Name)
	}
	parts := op.PathParts
	for i, part := range parts {
		if part.Param == nil {
			continue
		}
		startsSegment := i > 0 && strings.HasSuffix(parts[i-1].Raw, "/")
		endsSegment := i == len(parts)-1 || strings.HasPrefix(parts[i+1].Raw, "/")
		if !startsSegment || !endsSegment {
			return errors.Errorf("feature %q requires path parameter %q to span a whole path segment",
				ServerExternalMux.Name, part.Param.Spec.Name)
		}
	}
	names := op.MuxParamNames()
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return errors.Errorf("feature %q requires path parameters to have distinct wildcard names, %q is repeated",
				ServerExternalMux.Name, name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func wrapOperationLocation(op *ir.Operation, err error) error {
	pos, ok := op.Spec.Position()
	if !ok {
		return err
	}
	return &location.Error{
		File: op.Spec.File(),
		Pos:  pos,
		Err:  err,
	}
}

// muxRegisteredAt matches the generator source location that ServeMux
// adds to conflict panics, which is noise for spec authors.
var muxRegisteredAt = regexp.MustCompile(` \(registered at [^)]*\)`)

// checkExternalMuxConflicts registers every route on a throwaway
// http.ServeMux, so specs that ServeMux would reject at startup fail at
// generation instead, using ServeMux's own conflict rules.
func checkExternalMuxConflicts(ops []*ir.Operation) (rerr error) {
	mux := http.NewServeMux()
	var current *ir.Operation
	defer func() {
		if r := recover(); r != nil {
			msg := muxRegisteredAt.ReplaceAllString(fmt.Sprint(r), "")
			rerr = wrapOperationLocation(current, errors.Errorf("feature %q: %s", ServerExternalMux.Name, msg))
		}
	}()
	for _, op := range ops {
		current = op
		mux.Handle(strings.ToUpper(op.Spec.HTTPMethod)+" "+op.MuxPattern(), http.NotFoundHandler())
	}
	return nil
}
