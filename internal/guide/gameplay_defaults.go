// Copyright (c) 2026 Aristarh Ucolov.
package guide

// GameplayDefaults is cfggameplay.json as a fresh DayZ server ships it: every
// key the Gameplay editor knows, with the value the game uses when the file
// leaves the key out. The editor shows missing keys with these values, "Reset
// to defaults" restores them, and a mission without the file gets it from
// here.
//
// The monthly temperatures (WorldsData.environmentMinTemps/MaxTemps) are left
// out on purpose: they differ per map, and without them DayZ uses the map's
// own. "version" is the format version DayZ writes.
const GameplayDefaults = `{
	"version": 123,
	"GeneralData": {
		"disableBaseDamage": false,
		"disableContainerDamage": false,
		"disableRespawnDialog": false,
		"disableRespawnInUnconsciousness": false
	},
	"PlayerData": {
		"disablePersonalLight": false,
		"StaminaData": {
			"sprintStaminaModifierErc": 1.0,
			"sprintStaminaModifierCro": 1.0,
			"staminaWeightLimitThreshold": 6000.0,
			"staminaMax": 100.0,
			"staminaKgToStaminaPercentPenalty": 1.75,
			"staminaMinCap": 5.0,
			"sprintSwimmingStaminaModifier": 1.0,
			"sprintLadderStaminaModifier": 1.0,
			"meleeStaminaModifier": 1.0,
			"obstacleTraversalStaminaModifier": 1.0,
			"holdBreathStaminaModifier": 1.0
		},
		"ShockHandlingData": {
			"shockRefillSpeedConscious": 5.0,
			"shockRefillSpeedUnconscious": 1.0,
			"allowRefillSpeedModifier": true
		},
		"MovementData": {
			"timeToStrafeJog": 0.1,
			"rotationSpeedJog": 0.3,
			"timeToSprint": 0.45,
			"timeToStrafeSprint": 0.3,
			"rotationSpeedSprint": 0.15,
			"allowStaminaAffectInertia": true
		},
		"DrowningData": {
			"staminaDepletionSpeed": 10.0,
			"healthDepletionSpeed": 10.0,
			"shockDepletionSpeed": 10.0
		},
		"WeaponObstructionData": {
			"staticMode": 1,
			"dynamicMode": 1
		},
		"spawnGearPresetFiles": []
	},
	"WorldsData": {
		"lightingConfig": 0,
		"objectSpawnersArr": [],
		"wetnessWeightModifiers": [1.0, 1.0, 1.33, 1.66, 2.0]
	},
	"BaseBuildingData": {
		"HologramData": {
			"disableIsCollidingBBoxCheck": false,
			"disableIsCollidingPlayerCheck": false,
			"disableIsClippingRoofCheck": false,
			"disableIsBaseViableCheck": false,
			"disableIsCollidingGPlotCheck": false,
			"disableIsCollidingAngleCheck": false,
			"disableIsPlacementPermittedCheck": false,
			"disableHeightPlacementCheck": false,
			"disableIsUnderwaterCheck": false,
			"disableIsInTerrainCheck": false,
			"disableColdAreaBuildingCheck": false,
			"disallowedTypesInUnderground": ["FenceKit", "TerritoryFlagKit", "WatchtowerKit"]
		},
		"ConstructionData": {
			"disablePerformRoofCheck": false,
			"disableIsCollidingCheck": false,
			"disableDistanceCheck": false
		}
	},
	"UIData": {
		"use3DMap": false,
		"HitIndicationData": {
			"hitDirectionOverrideEnabled": false,
			"hitDirectionBehaviour": 1,
			"hitDirectionStyle": 0,
			"hitDirectionIndicatorColorStr": "0xffbb0a1e",
			"hitDirectionMaxDuration": 2.0,
			"hitDirectionBreakPointRelative": 0.2,
			"hitDirectionScatter": 10.0,
			"hitIndicationPostProcessEnabled": true
		}
	},
	"MapData": {
		"ignoreMapOwnership": false,
		"ignoreNavItemsOwnership": false,
		"displayPlayerPosition": false,
		"displayNavInfo": true
	},
	"VehicleData": {
		"boatDecayMultiplier": 1
	}
}`
