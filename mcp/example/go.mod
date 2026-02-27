module ergo.services/application/mcp/example

go 1.20

require (
	ergo.services/application/mcp v0.0.0
	ergo.services/ergo v1.999.320
)

replace (
	ergo.services/application/mcp => ../
	ergo.services/ergo => ../../../ergo
)
