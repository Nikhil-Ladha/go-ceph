package implements

import (
	"fmt"
	"runtime"
	"strings"

	"modernc.org/cc/v4"
)

var (
	// Individual "package" stubs. Add the needed headers to pick up the
	// ceph lib<whatever> content.

	cephfsCStub = `
#include "cephfs/libcephfs.h"
`
	radosCStub = `
#include "rados/librados.h"
`
	radosStriperCStub = `
#include "radosstriper/libradosstriper.h"
`
	rbdCStub = `
#include "rbd/librbd.h"
#include "rbd/features.h"
`
	typeStubs = `
#define int8_t int
#define int16_t int
#define int32_t int
#define int64_t int
#define uint8_t int
#define uint16_t int
#define uint32_t int
#define uint64_t int
#define dev_t int
#define size_t int
#define ssize_t int
#define mode_t int
#define uid_t int
#define gid_t int
#define off_t int
#define time_t int
#define bool int
#define __GNUC__ 4
#define __x86_64__ 1
#define __linux__ 1

int __predefined_declarator;
`
	stubs = map[string]string{
		"cephfs":        cephfsCStub,
		"rados":         radosCStub,
		"rados/striper": radosStriperCStub,
		"rbd":           rbdCStub,
	}
	funcPrefix = map[string]string{
		"cephfs": "ceph_",
		"rados":  "rados_",
		"rbd":    "rbd_",
	}
)

func stubCFunctions(libname string) (CFunctions, error) {
	cstub := stubs[libname]
	if cstub == "" {
		return nil, fmt.Errorf("no C stub available for '%s'", libname)
	}
	conf, err := cc.NewConfig(runtime.GOOS, runtime.GOARCH, "-D_FILE_OFFSET_BITS=64")
	if err != nil {
		return nil, err
	}

	src := []cc.Source{
		{Name: "<predefined>", Value: conf.Predefined},
		{Name: "<builtin>", Value: cc.Builtin},
		{Name: "typestubs", Value: typeStubs},
		{Name: libname, Value: cstub},
	}
	cAST, err := cc.Parse(conf, src)
	if err != nil {
		return nil, err
	}
	cfMap := map[cc.StringValue]CFunction{}
	for i := range cAST.Scope.Nodes {
		for _, n := range cAST.Scope.Nodes[i] {
			if n, ok := n.(*cc.Declarator); ok &&
				!n.IsTypename() &&
				n.DirectDeclarator != nil &&
				(n.DirectDeclarator.Case == cc.DirectDeclaratorFuncParam ||
					n.DirectDeclarator.Case == cc.DirectDeclaratorFuncIdent) {
				name := n.Name()
				if _, exists := cfMap[cc.StringValue(name)]; !exists {
					isDeprecated := false
					if strings.Contains(n.DirectDeclarator.IdentifierList.String(), "deprecated") {
						isDeprecated = true
					}
					cf := CFunction{
						Name:         name,
						IsDeprecated: isDeprecated,
					}
					cfMap[cc.StringValue(name)] = cf
				}
			}
		}
	}
	cfs := make(CFunctions, 0, len(cfMap))
	for _, cf := range cfMap {
		cfs = append(cfs, cf)
	}
	return cfs, nil
}

// CephCFunctions will extract C functions from the supplied package name
// and update the results within the code inspector.
func CephCFunctions(pkg string, ii *Inspector) error {
	logger.Printf("getting C AST for %s", pkg)
	f, err := stubCFunctions(pkg)
	if err != nil {
		return err
	}
	return ii.SetExpected(funcPrefix[pkg], f)
}
