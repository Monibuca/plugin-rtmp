module github.com/Monibuca/plugin-rtmp/v3

go 1.13

require (
	github.com/Monibuca/engine/v3 v3.0.1
	github.com/Monibuca/utils/v3 v3.0.0-alpha2
	github.com/logrusorgru/aurora v2.0.3+incompatible
)

replace github.com/Monibuca/engine/v3 => ../engine

replace github.com/Monibuca/utils/v3 v3.0.0-alpha2 => ../utils
