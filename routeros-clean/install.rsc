{
:if ([/system/resource/get architecture-name] != "arm64" || ([/system/resource/get version] != "7.25.1" && [:pick [/system/resource/get version] 0 7] != "7.25.1 ")) do={ :error "Supported target: ARM64 RouterOS 7.25.1" }
:if ([:len [/system/package/find where name="container" and disabled=no]] != 1 || ![/system/device-mode/get container]) do={ :error "Container package and device-mode required" }
:local m7Server [/ip/dhcp-server/find where disabled=no and invalid=no]
:if ([:len $m7Server] != 1) do={ :error "Cannot uniquely identify LAN DHCP server" }
:local m7Lan [/ip/dhcp-server/get $m7Server interface]
:local lanAddr [/ip/address/find where interface=$m7Lan and disabled=no and invalid=no]
:if ([:len $lanAddr] != 1) do={ :error "Cannot uniquely identify LAN router address" }
:local addrText [:tostr [/ip/address/get $lanAddr address]]
:local slash [:find $addrText "/"]
:local rip [:toip [:pick $addrText 0 $slash]]
:local bits [:tonum [:pick $addrText ($slash + 1) [:len $addrText]]]
:if ($bits < 16 || $bits > 30 || !(($rip in 10.0.0.0/8) || ($rip in 172.16.0.0/12) || ($rip in 192.168.0.0/16))) do={ :error "Unsupported LAN network" }
:local clientNet ([:tostr [/ip/address/get $lanAddr network]] . "/" . $bits)
:local m7Route [/ip/route/find where dst-address=0.0.0.0/0 and active=yes and routing-table=main]
:if ([:len $m7Route] != 1) do={ :error "WAN default route must be unique" }
:local gatewayText [:tostr [/ip/route/get $m7Route immediate-gw]]
:local perc [:find $gatewayText "%"]
:local m7Wan $gatewayText
:if ([:typeof $perc] = "num") do={ :set m7Wan [:pick $gatewayText ($perc + 1) [:len $gatewayText]] }
:if ($m7Wan = $m7Lan || [:len [/interface/find where name=$m7Wan and running=yes]] != 1) do={ :error "WAN path is ambiguous" }
:local m7Port 18443
:if ([:len [/ip/service/find where disabled=no and port=$m7Port]] > 0) do={ :error "Panel port in use" }
:foreach rule in=[/ip/firewall/nat/find where chain=dstnat and disabled=no] do={
:local configuredPort [:tostr [/ip/firewall/nat/get $rule dst-port]]
:local entryInterface [:tostr [/ip/firewall/nat/get $rule in-interface]]
:local ruleProto [:tostr [/ip/firewall/nat/get $rule protocol]]
:if (($entryInterface = "" || $entryInterface = $m7Lan || [:pick $entryInterface 0 1] = "!") && ($ruleProto = "" || $ruleProto = "tcp" || $ruleProto = "6")) do={
:local portConflict ($configuredPort = "")
:foreach entry in=[:toarray $configuredPort] do={
:local segment [:tostr $entry]
:local dash [:find $segment "-"]
:local low [:tonum $segment]
:local high $low
:if ([:typeof $dash] = "num") do={
:set low [:tonum [:pick $segment 0 $dash]]
:set high [:tonum [:pick $segment ($dash + 1) [:len $segment]]]
}
:if ([:typeof $low] != "num" || [:typeof $high] != "num") do={ :set portConflict true } else={
:if ($m7Port >= $low && $m7Port <= $high) do={ :set portConflict true }
}
}
:if ($portConflict) do={ :error "Existing destination NAT might use the panel port" }
}
}
:if ([:len [/ip/firewall/filter/find where action=fasttrack-connection and disabled=no]] != 0) do={ :error "Active FastTrack requires routing review" }
:local m7Gateway
:local be
:foreach n in={0;4;8;12;16;20;24;28} do={
:local prefix [:toip ("198.18.0." . $n)]
:local last [:toip ("198.18.0." . ($n + 3))]
:local collision false
:foreach r in=[/ip/route/find] do={
:local network [/ip/route/get $r dst-address]
:if ($network != 0.0.0.0/0 && (($prefix in $network) || ($last in $network))) do={ :set collision true }
:local existing [:tostr $network]
:local baseIP [:toip [:pick $existing 0 [:find $existing "/"]]]
:if ($network != 0.0.0.0/0 && $baseIP >= $prefix && $baseIP <= $last) do={ :set collision true }
}
:foreach a in=[/ip/address/find] do={
:local assigned [/ip/address/get $a address]
:local first [/ip/address/get $a network]
:if (($prefix in $assigned) || ($last in $assigned) || ($first >= $prefix && $first <= $last)) do={ :set collision true }
}
:if (!$collision && [:typeof $m7Gateway] = "nothing") do={
:set m7Gateway [:toip ("198.18.0." . ($n + 1))]
:set be [:toip ("198.18.0." . ($n + 2))]
}
}
:if ([:typeof $m7Gateway] != "ip") do={ :error "No unused container /30 available" }
:local m7Tag [:rndstr from="0123456789abcdef" length=32]
:local mown ("meridian-install:" . $m7Tag)
:local suffix [:pick $m7Tag 0 8]
:local m7Name ("m7c" . $suffix)
:local gn ("m7g" . $suffix)
:local br ("m7b" . $suffix)
:local m7Veth ("m7v" . $suffix)
:local m7Env ("m7e" . $suffix)
:local gateEnv ("m7q" . $suffix)
:local m7Mount ("m7m" . $suffix)
:local flash ""
:if ([:len [/file/find where name="flash" and type="disk"]] = 1) do={ :set flash "flash/" }
:local rootDir ($flash . $m7Name)
:local gateRoot ($flash . $gn)
:local gateArchive ($flash . $gn . ".tar")
:local stDir ($flash . "m7s" . $suffix)
:local m7ImagePath ($flash . $m7Name . ".tar")
:if ([:len [/container/find where name=$m7Name or name=$gn]] > 0 || [:len [/interface/find where name=$m7Veth or name=$br]] > 0 || [:len [/container/envs/find where list=$m7Env or list=$gateEnv]] > 0 || [:len [/container/mounts/find where list=$m7Mount]] > 0) do={ :error "Installation name collision" }
:foreach item in=($rootDir,$stDir,$m7ImagePath,$gateRoot,$gateArchive) do={ :if ([:len [/file/find where name=$item]] > 0) do={ :error "Installation path collision" } }
:local m7FetchImage do={
:local m7ImagePath $1
:local m7Url $2
:local archiveSize $3
:local sumRoot $4
:local m7SpaceAfter $5
:if ([/system/resource/get free-hdd-space] < ($archiveSize + $m7SpaceAfter)) do={ :error "Insufficient disk space for this image" }
/tool/fetch url=$m7Url dst-path=$m7ImagePath check-certificate=yes http-max-redirect-count=0 idle-timeout=30s
:local m7ImageOK false
:set m7ImageOK false
:local m7ChunkSize 32768
:local m7Files [/file/find where name=$m7ImagePath]
:if ([:len $m7Files] != 1) do={ :error "image missing or ambiguous" }
:if ([/file/get $m7Files size] != $archiveSize) do={ :error "image length mismatch" }
:local m7Offset 0
:local m7Acc "meridian-oci-v1:"
:while ($m7Offset < $archiveSize) do={
:local m7Want ($archiveSize - $m7Offset)
:if ($m7Want > $m7ChunkSize) do={ :set m7Want $m7ChunkSize }
:local m7Data ([/file/read file=$m7ImagePath offset=$m7Offset chunk-size=$m7Want as-value]->"data")
:if ([:len $m7Data] != $m7Want) do={ :error "image read truncated" }
:set m7Acc [:convert ($m7Acc . $m7Data) from=raw transform=sha512 to=hex]
:if ([:len $m7Acc] != 128) do={ :error "digest failed" }
:set m7Offset ($m7Offset + $m7Want)
}
:if ([/file/get $m7Files size] != $archiveSize) do={ :error "image changed size" }
:if ($m7Acc != $sumRoot) do={ :error "image digest mismatch" }
:set m7ImageOK true
:if (!$m7ImageOK || [/system/resource/get free-hdd-space] < $m7SpaceAfter) do={ :error "Archive verification or extraction space failed" }
:return true
}
:local m7WaitImport do={
:local candidateID $1
:local expectedID $2
:for i from=1 to=120 do={
:if ([/container/get $candidateID image-id] = $expectedID) do={ :return true }
:delay 2s
}
:error "Import did not match verified image"
}
:if ([:len [/container/find where name="m7c2a084c1d"]] > 0) do={
:local rr [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/0701f5281e0c6f09006c31b68d66fd12e9cdf62c/hap-retained-second.rsc" output=user as-value check-certificate=yes http-max-redirect-count=0 idle-timeout=30s]
:local rs ($rr->"data")
:if (($rr->"status")!="finished" || [:convert $rs from=raw transform=sha512 to=hex]!="166ead90d7387da9e3852a76f72cebefc64207fa1d40159c62e0e001cd039079e6b3b6cb069e39de49733eeda2350fc8659083a6d5c05a45f21a08d20de8c5ff") do={ :error "Previous installation recovery download failed" }
:local recover [:parse $rs]
$recover
}
:if ([:len [/container/find where comment~"^meridian-install:"]] > 0) do={ :error "Existing Meridian installation found; inspect it before retry" }
:local requiredBytes 88411648
:local availableBytes [/system/resource/get free-hdd-space]
:if ([:typeof $availableBytes] != "num" || $availableBytes < $requiredBytes) do={ :error ("Insufficient disk space: required " . $requiredBytes . " bytes, available " . $availableBytes) }
:local stablePath ($flash . "meridian-device-id.txt")
:local stableFile [/file/find where name=$stablePath]
:if ([:len $stableFile] = 0) do={
:local newDevice ("router-" . [:rndstr from="0123456789abcdef" length=32])
:onerror alreadyCreated in={ /file/add name=$stablePath type=file contents=$newDevice } do={}
:set stableFile [/file/find where name=$stablePath]
}
:if ([:len $stableFile] != 1 || ([/file/get $stableFile type] != ".txt file" && [/file/get $stableFile type] != "file") || [/file/get $stableFile size] != 39) do={ :error "Persistent Meridian router identity unavailable" }
:local deviceID [/file/get $stableFile contents]
:if ([:len $deviceID] != 39 || !($deviceID ~ "^router-[0-9a-f]+\$")) do={ :error "Invalid persistent Meridian router identity" }
:local gateCtx [:toarray ""]
:local m7RetireGate do={
:local gc $1
:local gateID ($gc->"gateID")
:local gn ($gc->"gateName")
:local mown ($gc->"owner")
:local m7Mount ($gc->"mount")
:local gateEnv ($gc->"gateEnv")
:local m7Veth ($gc->"veth")
:local gateImageID ($gc->"gateImageID")
:local gateEnvID ($gc->"gateEnvID")
:local gateArchive ($gc->"gateArchive")
:local originalRootID ($gc->"originalRootID")
:local gateRoot ($gc->"gateRoot")
:local deviceID ($gc->"deviceID")
:if ([/container/get $gateID image-id] != $gateImageID || [/container/get $gateID name] != $gn || [/container/get $gateID comment] != $mown || [:tostr [/container/get $gateID mountlists]] != $m7Mount || [:tostr [/container/get $gateID envlists]] != $gateEnv || [/container/get $gateID interface] != $m7Veth) do={ :error "Verifier ownership changed" }
:onerror alreadyStopped in={ /container/stop $gateID } do={}
:for i from=1 to=40 do={
:local existing [/container/find where name=$gn and comment=$mown]
:if ([:len $existing] = 0) do={ :break }
:if ([:len $existing] != 1 || [:tostr [:pick $existing 0]] != [:tostr $gateID] || [/container/get $gateID image-id] != $gateImageID) do={ :error "Verifier ownership changed" }
:onerror busy in={ /container/remove $gateID } do={}
:delay 1s
}
:if ([:len [/container/find where name=$gn]] > 0) do={ :error "Verifier shutdown incomplete" }
:local currentGateEnv [/container/envs/find where list=$gateEnv and key=M7_DEVICE_ID and value=$deviceID]
:if ([:len $currentGateEnv] != 1 || [:tostr [:pick $currentGateEnv 0]] != [:tostr $gateEnvID]) do={ :error "Verifier environment changed" }
/container/envs/remove $gateEnvID
:local oldArchive [/file/find where name=$gateArchive]
:if ([:len $oldArchive] != 1 || [:tostr [:pick $oldArchive 0]] != [:tostr ($gc->"archiveID")] || [/file/get $oldArchive size] != ($gc->"size")) do={ :error "Verifier archive ownership changed" }
/file/remove $oldArchive
:local remainingRoot [/file/find where name=$gateRoot]
:if ([:len $remainingRoot] > 0) do={
:if ([:len $remainingRoot] != 1 || [:tostr [:pick $remainingRoot 0]] != [:tostr $originalRootID] || [/file/get $remainingRoot type] != "directory") do={ :error "Verifier root changed" }
:foreach otherContainer in=[/container/find] do={
:local otherPath ([:tostr [/container/get $otherContainer root-dir]] . "/")
:if ([:pick $otherPath 0 ([:len $gateRoot] + 2)] = ("/" . $gateRoot . "/")) do={ :error "Verifier root in use" }
}
:foreach otherMount in=[/container/mounts/find] do={
:local otherPath ([:tostr [/container/mounts/get $otherMount src]] . "/")
:if ([:pick $otherPath 0 ([:len $gateRoot] + 2)] = ("/" . $gateRoot . "/")) do={ :error "Verifier root in use" }
}
/file/remove $remainingRoot
}
:return true
}
:local authorizePanel do={
:local containerName $1
:local stDir $2
:local rip $3
:local panelPort $4
:local attempt [:rndstr from="0123456789abcdef" length=32]
:local containers [/container/find where name=$containerName]
:if ([:len $containers] != 1) do={ :error "Meridian bootstrap container unavailable" }
:local m7C [:pick $containers 0]
:if ([/container/get $m7C logging] || [/container/get $m7C start-on-boot]) do={ :error "Meridian bootstrap terminal configuration mismatch" }
:local mountList [:tostr [/container/get $m7C mountlists]]
:local mounted [/container/mounts/find where list=$mountList]
:if ([:len $mounted] != 1) do={ :error "Meridian state mount mismatch" }
:local mountID [:pick $mounted 0]
:local source [:tostr [/container/mounts/get $mountID src]]
:if ([/container/mounts/get $mountID dst] != "/state" || ($source != ("/" . $stDir) && $source != $stDir)) do={ :error "Meridian state mount mismatch" }
:local gate ($stDir . "/gate-" . $attempt . ".ok")
:local used ($stDir . "/used-" . $attempt)
:local panel ($stDir . "/ui-identity")
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 0) || ([:len [/file/find where name=$panel]] != 0)) do={ :error "Meridian installation state already exists" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --authorize-install " . $attempt) no-sh
:if ([:len [/file/find where name=$gate]] != 1) do={ :error "Premium authorization incomplete" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --prepare-panel " . $attempt . " " . [:tostr $rip] . " " . [:tostr $panelPort]) no-sh
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 1)) do={ :error "Premium authorization receipt not consumed" }
:foreach suffix in={"panel.crt";"panel.key";"preview.auth"} do={
:local obj [/file/find where name=($panel . "/" . $suffix)]
:if ([:len $obj] != 1) do={ :error "Panel identity incomplete" }
:if ([/file/get [:pick $obj 0] size] < 1) do={ :error "Panel identity incomplete" }
}
:return true
}
:local netIDs [:toarray ""]
:local rh do={
:local observed $1
:if ([:typeof $observed] != "array" || [:len [:tostr ($observed->".id")]] = 0) do={ :error "Unable to fingerprint network object" }
:foreach field in={"bytes";"packets";"invalid";"hw-offloaded"} do={ :set ($observed->$field) }
:return [:convert [:serialize to=json value=$observed options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local pf do={
:local ident $1
:local lbl $2
:local first $3
:if ([/ip/firewall/filter/get $ident comment] != $lbl) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/filter/move $ident destination=$first }
/ip/firewall/filter/enable $ident
:return ([/ip/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceFilter6 do={
:local ident $1
:local lbl $2
:local first $3
:if ([/ipv6/firewall/filter/get $ident comment] != $lbl) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ipv6/firewall/filter/move $ident destination=$first }
/ipv6/firewall/filter/enable $ident
:return ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceNAT do={
:local ident $1
:local lbl $2
:local first $3
:if ([/ip/firewall/nat/get $ident comment] != $lbl) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/nat/move $ident destination=$first }
/ip/firewall/nat/enable $ident
:return ([/ip/firewall/nat/print as-value where .id=$ident]->0)
}
:local mdi [:toarray ""]
:local m7ModeV6
:local lockName ($flash . "meridian-install.lock")
:local lockID
:local releaseLock do={
:local lockFile $1
:local lockTag $2
:local lockPath $3
:local found [/file/find where name=$lockPath]
:if ([:len $found] != 1 || [:tostr [:pick $found 0]] != [:tostr $lockFile] || [/file/get $lockFile contents] != $lockTag) do={ :error "Meridian installation lock changed" }
/file/remove $lockFile
}
:local m7C
:put ("Meridian installation " . $m7Name)
:onerror failure in={
/file/add name=$lockName type=file contents=$m7Tag
:local held [/file/find where name=$lockName]
:if ([:len $held] != 1 || [/file/get $held contents] != $m7Tag) do={ :error "Concurrent installer or interrupted installation" }
:set lockID [:pick $held 0]
:if ([:len [/container/find where comment~"^meridian-install:"]] > 0) do={ :error "Existing Meridian installation found" }
:put "Meridian: downloading key verifier"
:if (![$m7FetchImage $gateArchive "https://raw.githubusercontent.com/JinComputers/meridian-linux/0701f5281e0c6f09006c31b68d66fd12e9cdf62c/meridian-hapax2-premium-0eef506e3d93acb400bec911f7d7e9172471451400a137fc14f19430864f3080.tar" 6656000 "febc6214f02f3fd38c7a3e4946452ee4cf76c45ede232e5c8ae99b10ab7fc67dfecf9ff1e877a37a362985ac72980c231de3986ee911c5d9bc43f94e34cdcf3e" 28014080]) do={ :error "Verifier download failed" }
/interface/bridge/add name=$br protocol-mode=none comment=$mown
:set ($gateCtx->"vethID") [/interface/veth/add name=$m7Veth address=([:tostr $be] . "/30") gateway=$m7Gateway comment=$mown]
/interface/bridge/port/add bridge=$br interface=$m7Veth comment=$mown
/ip/address/add address=([:tostr $m7Gateway] . "/30") interface=$br comment=$mown
{
:local lbl ($mown . "-drop-output")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward out-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-drop-input")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-drop-router")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=input in-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-dns-udp")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=input in-interface=$br src-address=$be dst-address-type=local protocol=udp dst-port=53 action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-dns-tcp")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=input in-interface=$br src-address=$be dst-address-type=local protocol=tcp dst-port=53 action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-drop6-router")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=input in-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-drop6-from")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward in-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-drop6-to")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward out-interface=$br action=drop comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-wan-return")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Wan out-interface=$br dst-address=$be connection-state=established,related action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-wan-out")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$br out-interface=$m7Wan src-address=$be action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-wan-nat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=srcnat out-interface=$m7Wan src-address=$be action=masquerade comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
:set ($gateCtx->"mountID") [/container/mounts/add list=$m7Mount src=$stDir dst=/state]
:local gateEnvID [/container/envs/add list=$gateEnv key=M7_DEVICE_ID value=$deviceID]
:if (![/ip/dns/get allow-remote-requests]) do={ :error "Router DNS access is disabled" }
/container/add name=$gn file=$gateArchive root-dir=$gateRoot interface=$m7Veth dns=$m7Gateway mountlists=$m7Mount envlists=$gateEnv entrypoint="/usr/local/bin/meridian-installer" logging=no start-on-boot=no comment=$mown
:local gateRows [/container/find where name=$gn and comment=$mown]
:if ([:len $gateRows] != 1) do={ :error "Verifier container creation uncertain" }
:local gateID [:pick $gateRows 0]
:local gateImageID "0dd3cb210da8d455026a5a16dc7af1673f4c56eef748cb58ec039c2d9b2b4ffb"
:if (![$m7WaitImport $gateID $gateImageID]) do={ :error "Verifier import failed" }
:local rootFiles [/file/find where name=$gateRoot]
:local originalRootID
:if ([:len $rootFiles] = 1) do={ :set originalRootID [:pick $rootFiles 0] }
:set ($gateCtx->"gateID") $gateID
:set ($gateCtx->"gateName") $gn
:set ($gateCtx->"owner") $mown
:set ($gateCtx->"mount") $m7Mount
:set ($gateCtx->"gateEnv") $gateEnv
:set ($gateCtx->"veth") $m7Veth
:set ($gateCtx->"gateImageID") $gateImageID
:set ($gateCtx->"gateEnvID") $gateEnvID
:set ($gateCtx->"gateArchive") $gateArchive
:set ($gateCtx->"originalRootID") $originalRootID
:set ($gateCtx->"gateRoot") $gateRoot
:set ($gateCtx->"deviceID") $deviceID
:set ($gateCtx->"size") 6656000
:set ($gateCtx->"archiveID") [:pick [/file/find where name=$gateArchive] 0]
/container/start $gateID
:local bootReady ($stDir . "/bootstrap-ready")
:for i from=1 to=40 do={ :if ([:len [/file/find where name=$bootReady]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$bootReady]] != 1) do={ :error "Verifier did not start" }
:if (![$authorizePanel $gn $stDir $rip $m7Port]) do={ :error "Premium authorization failed" }
:if (![$m7RetireGate $gateCtx]) do={ :error "Verifier retirement failed" }
:set ($gateCtx->"retired") true
:if ([:len [/file/find where name=($stDir . "/ui-identity/preview.auth")]] != 1) do={ :error "Authorized state unavailable" }
:put "Meridian: Premium verified; downloading VPN and panel"
:if (![$m7FetchImage $m7ImagePath "https://raw.githubusercontent.com/JinComputers/meridian-linux/42a80c751965105f5aab552432ea0f829c991100/routeros-clean/meridian-hapax2-panel-30b8bdb0.tar" 17684480 "d7828a83315a742a0a31621db97f879fed5b35e7e131cbded9c3b4779d48a61904d0297dd6ade6f0a734f6a4e70e133357cb9884256b7734a0399c8dac679103" 70727168]) do={ :error "Runtime download failed" }
/container/envs/add list=$m7Env key=M7_INGRESS_IFACE value=$m7Veth
/container/envs/add list=$m7Env key=M7_SOURCE_CIDR value=$clientNet
/container/envs/add list=$m7Env key=M7_PANEL_CIDR value=$clientNet
/container/envs/add list=$m7Env key=M7_PANEL_BIND value=([:tostr $be] . ":" . $m7Port)
/container/envs/add list=$m7Env key=M7_PANEL_AUTHORITY value=([:tostr $rip] . ":" . $m7Port)
/container/envs/add list=$m7Env key=M7_READY_ID value=$m7Tag
/container/add name=$m7Name file=$m7ImagePath root-dir=$rootDir interface=$m7Veth dns=$m7Gateway mountlists=$m7Mount envlists=$m7Env entrypoint="/usr/local/sbin/m7-image-entry" cmd=stack logging=no start-on-boot=no comment=$mown
:set m7C [/container/find where name=$m7Name and comment=$mown]
:if ([:len $m7C] != 1 || ![$m7WaitImport [:pick $m7C 0] "bcf1598ebf20be8d506982d00be89d3acf8fc7d40d9070dde57ec2b3c95a365e"]) do={ :error "VPN container import failed" }
:local stamp ($stDir . "/stack-ready-" . $m7Tag)
:if ([:len [/file/find where name=$stamp]] != 0) do={ :error "Unexpected startup marker" }
/container/start $m7C
:for i from=1 to=120 do={ :if ([:len [/file/find where name=$stamp]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$stamp]] != 1) do={
:foreach step in={"stack";"state-ok";"state-invalid";"identity-ok";"identity-invalid";"panel-exited";"edge-exited";"panel";"vpn-supervisor";"peer-ready";"peer-rate-limited";"peer-unavailable";"peer-unsupported";"peer-denied";"peer-failed";"engine-stopped";"tun-ready";"tun-timeout"} do={
:if ([:len [/file/find where name=($stDir . "/diag-" . $m7Tag . "-" . $step)]] = 1) do={ :put ("M7START " . $step) }
}
:error "VPN tunnel and panel startup unconfirmed"
}
{
:local lbl ($mown . "-panel-reply")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$br out-interface=$m7Lan src-address=$be dst-address=$clientNet protocol=tcp src-port=$m7Port connection-state=established connection-nat-state=dstnat action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-panel-access")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Lan out-interface=$br src-address=$clientNet dst-address=$be protocol=tcp dst-port=$m7Port connection-nat-state=dstnat action=accept comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$pf $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
{
:local lbl ($mown . "-panel-dnat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=dstnat in-interface=$m7Lan src-address=$clientNet dst-address=$rip protocol=tcp dst-port=$m7Port action=dst-nat to-addresses=$be to-ports=$m7Port comment=$lbl disabled=yes]
:set ($netIDs->$lbl) $ident
:set ($netIDs->($lbl . "-sha")) [$rh ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $lbl $first]
:set ($netIDs->($lbl . "-sha")) [$rh $actual]
}
:local receiptFile ($stDir . "/policy-host-status.json")
:local receiptReady ($stDir . "/host-receipt-ready-" . $m7Tag)
:if ([:len [/file/find where name=$receiptFile or name=$receiptReady]]!=0) do={ :error "Unexpected host receipt" }
/container/shell $m7C cmd=("/bin/sh -c \"umask 077; test ! -e /state/policy-host-status.json || exit 1; printf %s '{}' > /state/policy-host-status.json || exit 1; test \$(stat -c '%u:%a:%h' /state/policy-host-status.json) = 0:600:1 || exit 1; printf %s " . $m7Tag . " > /state/host-receipt-ready-" . $m7Tag . "\"") no-sh
:for i from=1 to=20 do={ :if ([:len [/file/find where name=$receiptReady]]=1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$receiptReady]]!=1 || [/file/get [:pick [/file/find where name=$receiptReady] 0] contents]!=$m7Tag) do={ :error "Host receipt preparation failed" }
{
:local fp do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local table ("m7t" . $suffix)
:local black ("m7b" . $suffix)
:local white ("m7w" . $suffix)
:local marker ("m7-" . $suffix . "-")
:local scoped ("^m7-" . $suffix . "-")
:local hold4 [/ip/firewall/raw/find where comment~$scoped]
:local hold6 [/ipv6/firewall/raw/find where comment~$scoped]
:if ([:len [/routing/table/find where name=$table]]>0 || [:len [/ip/firewall/mangle/find where chain=$black or chain=$white]]>0 || [:len [/ip/firewall/mangle/find where comment~$scoped]]>0 || [:len $hold4]>0 || [:len $hold6]>0) do={ :error "Mode names in use" }
:local layoutPath ($stDir . "/policy-layout.json")
:if ([:len [/file/find where name=$layoutPath]]>0) do={ :error "Policy layout already exists" }
:local count6 0
:foreach rid in=[/ipv6/route/find where active=yes and routing-table=main] do={
:if ([:tostr [/ipv6/route/get $rid gateway]] = [:tostr $m7Lan]) do={
:local prefix6 [/ipv6/route/get $rid dst-address]
:local text6 [:tostr $prefix6]
:local j [:find $text6 "/"]
:if ([:typeof $j] = "num") do={
:local host6 [:toip6 [:pick $text6 0 $j]]
:local b6 [:tonum [:pick $text6 ($j + 1) [:len $text6]]]
:if ([:typeof $host6] = "ip6" && $b6 >= 48 && $b6 <= 120 && !($host6 in fe80::/10) && !($host6 in ff00::/8)) do={
:set count6 ($count6 + 1)
:set m7ModeV6 $prefix6
}
}
}
}
:if ($count6 != 1) do={ :set m7ModeV6 }
:set ($mdi->"table") [/routing/table/add name=$table fib comment=($mown . "-policy-table")]
:set ($mdi->"sha-table") [$fp ([/routing/table/print as-value where .id=($mdi->"table")]->0)]
:set ($mdi->"kill") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table blackhole distance=254 comment=($mown . "-policy-kill")]
:set ($mdi->"sha-kill") [$fp ([/ip/route/print as-value where .id=($mdi->"kill")]->0)]
:set ($mdi->"via") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table gateway=([:tostr $be] . "@main") distance=1 comment=($mown . "-policy-via")]
:set ($mdi->"sha-via") [$fp ([/ip/route/print as-value where .id=($mdi->"via")]->0)]
:set ($mdi->"local-black") [/ip/firewall/mangle/add chain=$black dst-address-type=local action=accept comment=($marker . "local-black")]
:set ($mdi->"sha-local-black") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"local-black")]->0)]
:set ($mdi->"local-white") [/ip/firewall/mangle/add chain=$white dst-address-type=local action=accept comment=($marker . "local-white")]
:set ($mdi->"sha-local-white") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"local-white")]->0)]
:set ($mdi->"keep-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-black")]
:set ($mdi->"sha-keep-black") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"keep-black")]->0)]
:set ($mdi->"keep-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-white")]
:set ($mdi->"sha-keep-white") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"keep-white")]->0)]
:set ($mdi->"listed-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7b" . $suffix) action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "listed-black")]
:set ($mdi->"sha-listed-black") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"listed-black")]->0)]
:set ($mdi->"listed-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7d" . $suffix) action=accept comment=($marker . "listed-white")]
:set ($mdi->"sha-listed-white") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"listed-white")]->0)]
:set ($mdi->"default-white") [/ip/firewall/mangle/add chain=$white action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "default-white")]
:set ($mdi->"sha-default-white") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"default-white")]->0)]
:local guardBlack ("m7gb" . $suffix)
:local guardWhite ("m7gw" . $suffix)
:local replyMark ("m7ri" . $suffix)
:if ([:len [/ip/firewall/filter/find where chain=$guardBlack or chain=$guardWhite]]>0) do={ :error "Policy guard chain collision" }
:set ($mdi->"reply-new") [/ip/firewall/mangle/add chain=forward in-interface=$m7Wan out-interface=$m7Lan dst-address=$clientNet connection-state=new connection-mark=no-mark action=mark-connection new-connection-mark=$replyMark passthrough=yes disabled=yes comment=($marker . "reply-new")]
:set ($mdi->"sha-reply-new") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"reply-new")]->0)]
:set ($mdi->"reply-black") [/ip/firewall/mangle/add chain=$black connection-mark=$replyMark action=accept place-before=($mdi->"keep-black") comment=($marker . "reply-black")]
:set ($mdi->"sha-reply-black") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"reply-black")]->0)]
:set ($mdi->"reply-white") [/ip/firewall/mangle/add chain=$white connection-mark=$replyMark action=accept place-before=($mdi->"keep-white") comment=($marker . "reply-white")]
:set ($mdi->"sha-reply-white") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"reply-white")]->0)]
:set ($mdi->"gr-black") [/ip/firewall/filter/add chain=$guardBlack connection-mark=$replyMark action=return comment=($marker . "gr-black")]
:set ($mdi->"sha-gr-black") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gr-black")]->0)]
:set ($mdi->"gr-white") [/ip/firewall/filter/add chain=$guardWhite connection-mark=$replyMark action=return comment=($marker . "gr-white")]
:set ($mdi->"sha-gr-white") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gr-white")]->0)]
:set ($mdi->"gk-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-black")]
:set ($mdi->"sha-gk-black") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gk-black")]->0)]
:set ($mdi->"gk-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-white")]
:set ($mdi->"sha-gk-white") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gk-white")]->0)]
:set ($mdi->"gl-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7b" . $suffix) out-interface=("!" . $br) action=drop comment=($marker . "gl-black")]
:set ($mdi->"sha-gl-black") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gl-black")]->0)]
:set ($mdi->"gl-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7d" . $suffix) action=return comment=($marker . "gl-white")]
:set ($mdi->"sha-gl-white") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gl-white")]->0)]
:set ($mdi->"gd-white") [/ip/firewall/filter/add chain=$guardWhite out-interface=("!" . $br) action=drop comment=($marker . "gd-white")]
:set ($mdi->"sha-gd-white") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gd-white")]->0)]
:local allForward [/ip/firewall/filter/find where chain=forward]
:set ($mdi->"gentry") [/ip/firewall/filter/add chain=forward in-interface=$m7Lan src-address=$clientNet action=jump jump-target=$guardBlack disabled=yes place-before=[:pick $allForward 0] comment=($marker . "gentry")]
:set ($mdi->"sha-gentry") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"gentry")]->0)]
:set ($mdi->"lan-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$clientNet comment=($mown . "-policy-keep-lan")]
:set ($mdi->"sha-lan-keep") [$fp ([/ip/firewall/address-list/print as-value where .id=($mdi->"lan-keep")]->0)]
:local linkAddr [/ip/address/find where interface=$br and comment=$mown]
:if ([:len $linkAddr] != 1) do={ :error "VPN link address unavailable" }
:local linkNetwork [:tostr [/ip/address/get [:pick $linkAddr 0] network]]
:set ($mdi->"link-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=($linkNetwork . "/30") comment=($mown . "-policy-keep-link")]
:set ($mdi->"sha-link-keep") [$fp ([/ip/firewall/address-list/print as-value where .id=($mdi->"link-keep")]->0)]
:local n 0
:foreach aid in=[/ip/address/find where disabled=no and invalid=no] do={
:local iface [:tostr [/ip/address/get $aid interface]]
:if ($iface!=[:tostr $m7Wan] && $iface!=[:tostr $br] && $iface!=[:tostr $m7Lan]) do={
:local a [:tostr [/ip/address/get $aid address]]
:local slash [:find $a "/"]
:if ([:typeof $slash]!="num") do={ :error "Incomplete connected subnet" }
:local bits [:tonum [:pick $a ($slash+1) [:len $a]]]
:if ($bits<8 || $bits>32 || $n>=128) do={ :error "Unsupported connected subnet" }
:local prefix ([:tostr [/ip/address/get $aid network]] . "/" . $bits)
:local keepName ("m7k" . $suffix)
:if ([:len [/ip/firewall/address-list/find where list=$keepName and address=$prefix]]>0) do={ :continue }
:local key ("connected-" . $n)
:local item [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$prefix comment=($mown . "-" . $key)]
:set ($mdi->$key) $item
:set ($mdi->("sha-" . $key)) [$fp ([/ip/firewall/address-list/print as-value where .id=$item]->0)]
:set n ($n+1)
}
}
:set ($mdi->"entry") [/ip/firewall/mangle/add chain=prerouting in-interface=$m7Lan src-address=$clientNet action=jump jump-target=$black disabled=yes comment=($marker . "entry")]
:set ($mdi->"sha-entry") [$fp ([/ip/firewall/mangle/print as-value where .id=($mdi->"entry")]->0)]
:local denyOut [/ip/firewall/filter/find where comment=($mown . "-drop-output")]
:local denyIn [/ip/firewall/filter/find where comment=($mown . "-drop-input")]
:if ([:len $denyOut] != 1 || [:len $denyIn] != 1) do={ :error "Missing container boundary" }
:set ($mdi->"vpn-out") [/ip/firewall/filter/add chain=forward in-interface=$m7Lan out-interface=$br src-address=$clientNet dst-address=("!" . [:tostr $be]) routing-mark=$table action=accept place-before=[:pick $denyOut 0] disabled=yes comment=($mown . "-policy-vpn-out")]
:set ($mdi->"sha-vpn-out") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"vpn-out")]->0)]
:set ($mdi->"vpn-in") [/ip/firewall/filter/add chain=forward in-interface=$br out-interface=$m7Lan src-address=("!" . [:tostr $be]) dst-address=$clientNet connection-state=established,related action=accept place-before=[:pick $denyIn 0] disabled=yes comment=($mown . "-policy-vpn-in")]
:set ($mdi->"sha-vpn-in") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"vpn-in")]->0)]
:set ($mdi->"panel-reject") [/ip/firewall/filter/add chain=forward in-interface=$br out-interface=$m7Lan src-address=$be dst-address=$clientNet protocol=tcp src-port=$m7Port action=drop place-before=[:pick $denyIn 0] disabled=yes comment=($mown . "-policy-panel-reject")]
:set ($mdi->"sha-panel-reject") [$fp ([/ip/firewall/filter/print as-value where .id=($mdi->"panel-reject")]->0)]
:set ($mdi->"panel-out") [/ip/firewall/raw/add chain=prerouting in-interface=$m7Lan src-address=$clientNet dst-address=$rip protocol=tcp dst-port=$m7Port action=accept disabled=yes comment=($mown . "-policy-panel-out")]
:set ($mdi->"sha-panel-out") [$fp ([/ip/firewall/raw/print as-value where .id=($mdi->"panel-out")]->0)]
:set ($mdi->"panel-in") [/ip/firewall/raw/add chain=prerouting in-interface=$br src-address=$be dst-address=$clientNet protocol=tcp src-port=$m7Port action=accept disabled=yes comment=($mown . "-policy-panel-in")]
:set ($mdi->"sha-panel-in") [$fp ([/ip/firewall/raw/print as-value where .id=($mdi->"panel-in")]->0)]
:set ($mdi->"router-in") [/ip/firewall/raw/add chain=prerouting in-interface=$m7Lan src-address=$clientNet dst-address=$rip action=accept disabled=yes comment=($mown . "-policy-router-in")]
:set ($mdi->"sha-router-in") [$fp ([/ip/firewall/raw/print as-value where .id=($mdi->"router-in")]->0)]
:set ($mdi->"out4") [/ip/firewall/raw/add chain=prerouting src-address=$clientNet action=drop disabled=yes comment=($marker . "out4")]
:set ($mdi->"sha-out4") [$fp ([/ip/firewall/raw/print as-value where .id=($mdi->"out4")]->0)]
:set ($mdi->"in4") [/ip/firewall/raw/add chain=prerouting dst-address=$clientNet action=drop disabled=yes comment=($marker . "in4")]
:set ($mdi->"sha-in4") [$fp ([/ip/firewall/raw/print as-value where .id=($mdi->"in4")]->0)]
:if ($count6 = 1) do={
:set ($mdi->"out6") [/ipv6/firewall/raw/add chain=prerouting src-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "out6")]
:set ($mdi->"sha-out6") [$fp ([/ipv6/firewall/raw/print as-value where .id=($mdi->"out6")]->0)]
:set ($mdi->"in6") [/ipv6/firewall/raw/add chain=prerouting dst-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "in6")]
:set ($mdi->"sha-in6") [$fp ([/ipv6/firewall/raw/print as-value where .id=($mdi->"in6")]->0)]
}
:set ($mdi->"v6-from") [/ipv6/firewall/filter/add chain=forward in-interface=$m7Lan action=drop disabled=yes comment=($mown . "-policy-v6-from")]
:set ($mdi->"sha-v6-from") [$fp ([/ipv6/firewall/filter/print as-value where .id=($mdi->"v6-from")]->0)]
:set ($mdi->"v6-to") [/ipv6/firewall/filter/add chain=forward out-interface=$m7Lan action=drop disabled=yes comment=($mown . "-policy-v6-to")]
:set ($mdi->"sha-v6-to") [$fp ([/ipv6/firewall/filter/print as-value where .id=($mdi->"v6-to")]->0)]
:local layout [:toarray ""]
:set ($layout->"version") 1
:set ($layout->"phase") "staged"
:set ($layout->"tag") $m7Tag
:set ($layout->"mode_tag") $suffix
:set ($layout->"source") $clientNet
:set ($layout->"ipv6") [:tostr $m7ModeV6]
:set ($layout->"lan") $m7Lan
:set ($layout->"link") $br
:set ($layout->"veth") $m7Veth
:set ($layout->"wan") $m7Wan
:set ($layout->"backend") [:tostr $be]
:set ($layout->"router") [:tostr $rip]
:set ($layout->"port") $m7Port
:local refs [:toarray ""]
:foreach k,id in=$mdi do={ :if ([:pick $k 0 4] != "sha-") do={ :set ($refs->$k) [:tostr $id] } }
:set ($layout->"objects") $refs
:local body [:serialize to=json value=$layout options=json.no-string-conversion]
/file/add name=$layoutPath type=file contents=$body
:local created [/file/find where name=$layoutPath]
:if ([:len $created] != 1 || [/file/get [:pick $created 0] contents] != $body) do={ :error "Policy layout not persisted" }
:set ($mdi->"layout") [:pick $created 0]
:set ($mdi->"digest") [:convert $body from=raw transform=sha512 to=hex]
:local hostLayout ($flash . "m7h" . $suffix . ".json")
:local hostState ($flash . "m7x" . $suffix . ".json")
:local hostCtl ("m7ctl" . $suffix)
:if ([:len [/file/find where name=$hostLayout or name=$hostState]]>0 || [:len [/system/script/find where name=$hostCtl]]>0 || [:len [/system/scheduler/find where name=$hostCtl]]>0) do={ :error "Policy controller ownership collision" }
/file/add name=$hostLayout type=file contents=$body
:set ($mdi->"host-layout") [:pick [/file/find where name=$hostLayout] 0]
:local initial "{\"phase\":\"stable\",\"mode\":\"off\",\"digest\":\"\",\"list\":\"\",\"last\":\"\"}"
/file/add name=$hostState type=file contents=$initial
:set ($mdi->"host-state") [:pick [/file/find where name=$hostState] 0]
:set ($mdi->"initial") $initial
:local fetched [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/42a80c751965105f5aab552432ea0f829c991100/routeros-clean/native-mode-dispatch.rsc" output=user as-value check-certificate=yes http-max-redirect-count=0 idle-timeout=30s]
:local program ($fetched->"data")
:if (($fetched->"status")!="finished" || [:convert $program from=raw transform=sha512 to=hex]!="e63c8c49f087ee76a34998e54cf43d039dcba90a63c180507b2d3765d80fd2fb87424d278ff0ed52d73679edf740fb9c12f5ba85abe3db0df2613a06c6bbf7d4") do={ :error "Policy controller checksum mismatch" }
:set ($mdi->"script") [/system/script/add name=$hostCtl source=$program policy=ftp,read,write,test,sensitive comment=($mown . "-policy-controller")]
:set ($mdi->"scheduler") [/system/scheduler/add name=$hostCtl interval=10s start-date=[/system/clock/get date] start-time=[/system/clock/get time] on-event=$hostCtl policy=ftp,read,write,test,sensitive comment=($mown . "-policy-controller")]
:put ("M7POLICY staged source=" . $clientNet . " link=" . $br)
}
:local loginStamp ($stDir . "/panel-login-" . $m7Tag)
:if ([:len [/file/find where name=$loginStamp]] != 0) do={ :error "Unexpected login marker" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --panel-login " . [:tostr $rip] . " " . [:tostr $m7Port]) no-sh
:if ([:len [/file/find where name=$loginStamp]] != 1) do={ :error "Panel login was not delivered" }
/container/set $m7C start-on-boot=yes
$releaseLock $lockID $m7Tag $lockName
:put ("M7INSTALL stack-ready name=" . $m7Name . " panel=https://" . [:tostr $rip] . ":" . $m7Port . "/")
} do={
:put ("M7INSTALL interrupted: " . $failure)
:if ([:typeof $lockID] != "id") do={ :error "Meridian installer lock unavailable" }
:local attached [/interface/bridge/port/find where interface=$m7Veth and bridge=$br and comment=$mown]
:if ([:len $attached] = 1) do={ /interface/bridge/port/disable $attached }
:foreach objectName in=($gn,$m7Name) do={
:local current [/container/find where name=$objectName and comment=$mown]
:if ([:len $current] = 1) do={ :onerror e in={ /container/set $current start-on-boot=no; /container/stop $current } do={} }
}
{
:local fp do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local hostState ($mdi->"host-state")
:if ([:typeof $hostState]="id") do={
:if ([/file/get $hostState contents]!=($mdi->"initial")) do={ :error "Active host policy requires reversal before cleanup" }
}
:local worker ("m7ctl" . $suffix)
:local schedulerID ($mdi->"scheduler")
:if ([:typeof $schedulerID]="id") do={
:if ([/system/scheduler/get $schedulerID comment]!=($mown . "-policy-controller") || [/system/scheduler/get $schedulerID name]!=$worker || [:tostr [/system/scheduler/get $schedulerID on-event]]!=$worker) do={ :error "Scheduler ownership changed" }
/system/scheduler/remove $schedulerID
}
:local workerID ($mdi->"script")
:if ([:typeof $workerID]="id") do={
:if ([:len [/system/script/job/find where script=$worker]]>0 || [/system/script/get $workerID comment]!=($mown . "-policy-controller") || [/system/script/get $workerID name]!=$worker) do={ :error "Policy worker is busy or changed" }
/system/script/remove $workerID
}
:local hostConfig ($mdi->"host-layout")
:if ([:typeof $hostConfig]="id") do={
:if ([:convert [/file/get $hostConfig contents] from=raw transform=sha512 to=hex]!=($mdi->"digest")) do={ :error "Host policy layout changed" }
/file/remove $hostConfig
}
:if ([:typeof $hostState]="id") do={
/file/remove $hostState
}
:local f ($mdi->"layout")
:if ([:typeof $f] = "id") do={
:local files [/file/find where name=($stDir . "/policy-layout.json")]
:if ([:len $files] != 1 || [:tostr [:pick $files 0]] != [:tostr $f]) do={ :error "Policy layout ownership changed" }
:if ([:convert [/file/get $f contents] from=raw transform=sha512 to=hex] != ($mdi->"digest")) do={ :error "Policy layout changed" }
/file/remove $f
}
:foreach role in={"v6-to";"v6-from"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/filter/remove $id
}
}
:foreach role in={"in6";"out6"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/raw/remove $id
}
}
:foreach role in={"in4";"out4";"router-in";"panel-in";"panel-out"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/raw/remove $id
}
}
:foreach role in={"gentry";"gd-white";"gl-white";"gl-black";"gk-white";"gk-black";"gr-white";"gr-black";"panel-reject";"vpn-in";"vpn-out"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/filter/remove $id
}
}
:foreach role in={"entry";"reply-white";"reply-black";"reply-new";"default-white";"listed-white";"listed-black";"keep-white";"keep-black";"local-white";"local-black"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/mangle/remove $id
}
}
:foreach role,id in=$mdi do={
:if ([:pick $role 0 10]="connected-" && [:typeof $id]="id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows]!=1 || [$fp ($rows->0)]!=($mdi->("sha-" . $role))) do={ :error "Connected network ownership changed" }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"link-keep";"lan-keep"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"via";"kill"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/route/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/route/remove $id
}
}
:foreach role in={"table"} do={
:local id ($mdi->$role)
:if ([:typeof $id] = "id") do={
:local rows [/routing/table/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fp ($rows->0)] != ($mdi->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/routing/table/remove $id
}
}
}
:foreach part in={"panel-dnat";"wan-nat"} do={
:local lbl ($mown . "-" . $part)
:local ident ($netIDs->$lbl)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $lbl || [$rh ($rows->0)] != ($netIDs->($lbl . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/nat/remove $ident
}
}
:foreach part in={"panel-access";"panel-reply";"wan-out";"wan-return";"dns-tcp";"dns-udp";"drop-router";"drop-input";"drop-output"} do={
:local lbl ($mown . "-" . $part)
:local ident ($netIDs->$lbl)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $lbl || [$rh ($rows->0)] != ($netIDs->($lbl . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/filter/remove $ident
}
}
:foreach part in={"drop6-to";"drop6-from";"drop6-router"} do={
:local lbl ($mown . "-" . $part)
:local ident ($netIDs->$lbl)
:if ([:typeof $ident] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $lbl || [$rh ($rows->0)] != ($netIDs->($lbl . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ipv6/firewall/filter/remove $ident
}
}
:local ownedIP [/ip/address/find where interface=$br and comment=$mown and address=([:tostr $m7Gateway] . "/30")]
:if ([:len $ownedIP] = 1) do={ /ip/address/remove $ownedIP }
:if ([:len $attached] = 1 && [/interface/bridge/port/get $attached comment] = $mown && [/interface/bridge/port/get $attached bridge] = $br && [/interface/bridge/port/get $attached interface] = $m7Veth) do={ /interface/bridge/port/remove $attached }
:local createdBridge [/interface/bridge/find where name=$br and comment=$mown]
:if ([:len $createdBridge] = 1 && [:len [/interface/bridge/port/find where bridge=$br]] = 0 && [:len [/ip/address/find where interface=$br]] = 0 && [:len [/ipv6/address/find where interface=$br and dynamic=no]] = 0) do={ /interface/bridge/remove $createdBridge }
:local knownGate ($gateCtx->"gateID")
:if ([:typeof $knownGate] = "id" && ($gateCtx->"retired") != true && [:len [/file/find where name=($stDir . "/key") or name=($stDir . "/ui-identity")]] = 0) do={
:if (![$m7RetireGate $gateCtx]) do={ :error "Verifier recovery required" }
:local oldMount [/container/mounts/find where list=$m7Mount and dst="/state"]
:local oldLink [/interface/veth/find where name=$m7Veth and comment=$mown]
:if ([:len $oldMount] != 1 || [:tostr [:pick $oldMount 0]] != [:tostr ($gateCtx->"mountID")] || [:len $oldLink] != 1 || [:tostr [:pick $oldLink 0]] != [:tostr ($gateCtx->"vethID")] || [:len [/container/find where interface=$m7Veth]] > 0) do={ :error "Verifier mount or VETH ownership changed" }
:foreach other in=[/container/find] do={
:local lists ("," . [:tostr [/container/get $other mountlists]] . ",")
:if ([:typeof [:find $lists ("," . $m7Mount . ",")]] = "num") do={ :error "Verifier mount still in use" }
}
/container/mounts/remove $oldMount
/interface/veth/remove $oldLink
:local clean true
:local prefix ($stDir . "/")
:foreach member in=[/file/find] do={
:local pathName [/file/get $member name]
:if ([:pick $pathName 0 [:len $prefix]] = $prefix && $pathName != ($prefix . "bootstrap-ready") && $pathName != ($prefix . "device-id")) do={ :set clean false }
}
:local saved [/file/find where name=($prefix . "device-id")]
:if ($clean && [:len $saved] = 1 && [/file/get $saved contents] = ($deviceID . "\n")) do={
:local sd [/file/find where name=$stDir]
:if ([:len $sd] = 1 && [/file/get $sd type] = "directory") do={ /file/remove $sd }
}
$releaseLock $lockID $m7Tag $lockName
:put "M7INSTALL verifier removed; network restored; retry is available"
:error "Meridian installation incomplete"
}
$releaseLock $lockID $m7Tag $lockName
:put ("M7INSTALL network rules reverted; retained installation " . $m7Name . " gate " . $gn . " state " . $stDir . " archive " . $m7ImagePath)
:error "Meridian installation incomplete"
}
}
