module github.com/Biisairo/dynamic_react/L1_inject/inject

go 1.23

require (
	github.com/Biisairo/dynamic_react/L0_core/contract v0.0.0
	github.com/Biisairo/dynamic_react/L0_core/core v0.0.0
)

replace github.com/Biisairo/dynamic_react/L0_core/contract => ../../L0_core/contract

replace github.com/Biisairo/dynamic_react/L0_core/core => ../../L0_core/core
