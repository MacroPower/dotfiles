package bungie

// Inventory bucket hashes that the inventory logic treats specially.
const (
	BucketVault      uint32 = 138197802
	BucketPostmaster uint32 = 215593132
)

// Bucket hashes that the search filters, the preview population, and the
// saved loadouts read. Each value is a BucketHashes value of Destiny Item
// Manager (DIM).
const (
	BucketKinetic       uint32 = 1498876634
	BucketEnergy        uint32 = 2465295065
	BucketPower         uint32 = 953998645
	BucketHelmet        uint32 = 3448274439
	BucketGauntlets     uint32 = 3551918588
	BucketChestArmor    uint32 = 14239492
	BucketLegArmor      uint32 = 20886954
	BucketClassArmor    uint32 = 1585787867
	BucketEngrams       uint32 = 375726501
	BucketMessages      uint32 = 3161908920
	BucketSpecialOrders uint32 = 1367666825
	BucketEmotes        uint32 = 1107761855
	BucketFinishers     uint32 = 3683254069
	BucketEmblems       uint32 = 4274335291
	BucketArtifacts     uint32 = 1506418338
	BucketSubclass      uint32 = 3284755031
	BucketGhost         uint32 = 4023194814
	BucketShips         uint32 = 284967655
	BucketVehicle       uint32 = 2025709351
	BucketAccessories   uint32 = 687325600
	BucketConsumables   uint32 = 1469714392
	BucketModifications uint32 = 3313201758

	// BucketForbidden and BucketDummy hold the dummy items that DIM
	// leaves out of every store. DIM names them THE_FORBIDDEN_BUCKET
	// and SOME_OTHER_DUMMY_BUCKET.
	BucketForbidden uint32 = 2422292810
	BucketDummy     uint32 = 3621873013
)

// ItemLocationPostmaster is the Postmaster value of the Bungie ItemLocation
// enum.
const ItemLocationPostmaster = 4

// Bit flags of the Bungie TransferStatuses and ItemState enums.
const (
	TransferNotTransferable = 2

	StateLocked     = 1
	StateMasterwork = 4
	StateCrafted    = 8
)

// StackInstanceID is the item id that Bungie expects for an item without
// an instance.
const StackInstanceID = "0"

// Bungie DestinyItemType values that classify gear and plugs. Bungie
// names the plug type Mod, and weapon perks carry it too.
const (
	ItemTypeArmor  = 2
	ItemTypeWeapon = 3
	ItemTypePlug   = 19
)

// Bungie DestinyClass values. ClassAny is the value of an item that every
// class can use.
const (
	ClassTitan   = 0
	ClassHunter  = 1
	ClassWarlock = 2
	ClassAny     = 3
)

// Bungie BucketCategory values of the buckets that hold items that a player
// sees and manages.
const (
	BucketCategoryItem       = 1
	BucketCategoryEquippable = 3
)

// BucketScopeAccount is the Bungie BucketScope value of a bucket that every
// character shares.
const BucketScopeAccount = 1

// Bungie DestinyAmmunitionType values. An item without an equipping block
// has the ammo type zero.
const (
	AmmoPrimary = 1
	AmmoSpecial = 2
	AmmoHeavy   = 3
)

// Bungie TierType values, named after the rarity that DIM gives each value.
// The value of Common is TierTypeBasic.
const (
	TierTypeUnknown  = 0
	TierTypeCurrency = 1

	// TierTypeBasic is the value that the definition loader stores for a
	// definition without a tier type. DIM names the value Common, and it
	// also reads a missing tier type as Common.
	TierTypeBasic = 2

	TierTypeUncommon  = 3
	TierTypeRare      = 4
	TierTypeLegendary = 5
	TierTypeExotic    = 6
)

// Item category hashes that the search filters read. The server also reads
// them to change the categories of a definition the way DIM does. Each
// value is an ItemCategoryHashes value of DIM. A special grenade launcher
// holds the negative of CategoryGrenadeLaunchers.
const (
	CategoryWeapon                  int64 = 1
	CategoryPowerWeapon             int64 = 4
	CategoryAutoRifle               int64 = 5
	CategoryHandCannon              int64 = 6
	CategoryPulseRifle              int64 = 7
	CategoryScoutRifle              int64 = 8
	CategoryFusionRifle             int64 = 9
	CategorySniperRifle             int64 = 10
	CategoryShotgun                 int64 = 11
	CategoryMachineGun              int64 = 12
	CategoryRocketLauncher          int64 = 13
	CategorySidearm                 int64 = 14
	CategorySword                   int64 = 54
	CategoryGrenadeLaunchers        int64 = 153950757
	CategoryShipModsTransmatEffects int64 = 208981632
	CategoryWeaponMods              int64 = 610365472
	CategoryLinearFusionRifles      int64 = 1504945536
	CategoryReputationTokens        int64 = 2088636411
	CategoryTraceRifles             int64 = 2489664120
	CategoryDummies                 int64 = 3109687656
	CategoryBows                    int64 = 3317538576
	CategoryGlaives                 int64 = 3871742104
	CategorySubmachineGuns          int64 = 3954685534
	CategoryArmorMods               int64 = 4104513227
)

// Bungie DamageType values that the element filters read. DIM names the
// value DamageThermal solar. The value 5, which Bungie names Raid, has no
// filter.
const (
	DamageKinetic = 1
	DamageArc     = 2
	DamageThermal = 3
	DamageVoid    = 4
	DamageStasis  = 6
	DamageStrand  = 7
)

// Values of the uiStyle of a Bungie DestinyActivityRewardItem that the
// activity rows read. The game shows an extra engram as a bonus drop, and
// a daily grind reward as the focused reward of the activity.
const (
	RewardBonusDrop = "extra_engram"
	RewardFocus     = "daily_grind_guaranteed"
)
