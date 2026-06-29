module ergo.services/application/maestro

go 1.23

require (
	ergo.services/actor/saga v0.0.0
	ergo.services/ergo v1.999.321-0.20260608202150-ed4e48600507
)

replace ergo.services/ergo => ../../ergo

replace ergo.services/actor/saga => ../../actor/saga
