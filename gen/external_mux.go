package gen

import (
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
	return nil
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
