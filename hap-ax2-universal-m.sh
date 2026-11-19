#!/bin/sh
# m.sh — запуск установщика Meridian одной строкой (владелец 29.09.2026:
# «надо сократить до одной строчки запуск скрипта и прочее что в нем уже
# зашито»). Команда для человека (бот, письмо):
#
#   wget -qO- http://45.10.247.23/bin/m.sh | sh
#
# Адрес — RU-зеркало по HTTP: его берёт даже голый busybox wget без SSL на
# Keenetic, и оно доступно из России. Дальше всё здесь:
#  - install.sh качается по очереди из RU-зеркала, GitHub (HTTPS), Парижа —
#    curl, если есть, иначе wget (17.10.2026: RU первым, как в install.sh 5.62 —
#    под белыми списками на 4G живо только оно); на экран — куда подключаемся
#    и что качаем, чтобы не казалось, что завис;
#  - сверка с SHA256SUMS ТОГО ЖЕ источника (обрыв, битый кэш); не сошлось —
#    следующий источник;
#  - запуск install.sh с вводом С КЛАВИАТУРЫ (/dev/tty): через «| sh» ввод
#    самого sh занят этим файлом, а установщик спрашивает ключ и хэши.
# wget-ssl/ca-certificates здесь не ставим: install.sh сам чинит загрузчик
# для остальных файлов (install.sh: «Загрузчик без HTTPS…»).
#
# Всё внутри main и вызов в последней строке: при «| sh» файл читается по
# мере прихода, оборванная загрузка не должна исполнить половину.

meridian_routeros_script() {
cat <<'MERIDIAN_ROUTEROS_RSC'
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
:local routerIP [:toip [:pick $addrText 0 $slash]]
:local bits [:tonum [:pick $addrText ($slash + 1) [:len $addrText]]]
:if ($bits < 16 || $bits > 30 || !(($routerIP in 10.0.0.0/8) || ($routerIP in 172.16.0.0/12) || ($routerIP in 192.168.0.0/16))) do={ :error "Unsupported LAN network" }
:local clientNetwork ([:tostr [/ip/address/get $lanAddr network]] . "/" . $bits)
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
:local m7Backend
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
:set m7Backend [:toip ("198.18.0." . ($n + 2))]
}
}
:if ([:typeof $m7Gateway] != "ip") do={ :error "No unused container /30 available" }
:local m7Tag [:rndstr from="0123456789abcdef" length=32]
:local m7Owner ("meridian-install:" . $m7Tag)
:local suffix [:pick $m7Tag 0 8]
:local m7Name ("m7c" . $suffix)
:local gateName ("m7g" . $suffix)
:local m7Bridge ("m7b" . $suffix)
:local m7Veth ("m7v" . $suffix)
:local m7Env ("m7e" . $suffix)
:local gateEnv ("m7q" . $suffix)
:local m7Mount ("m7m" . $suffix)
:local flash ""
:if ([:len [/file/find where name="flash" and type="disk"]] = 1) do={ :set flash "flash/" }
:local rootDir ($flash . $m7Name)
:local gateRoot ($flash . $gateName)
:local gateArchive ($flash . $gateName . ".tar")
:local stateDir ($flash . "m7s" . $suffix)
:local m7ImagePath ($flash . $m7Name . ".tar")
:if ([:len [/container/find where name=$m7Name or name=$gateName]] > 0 || [:len [/interface/find where name=$m7Veth or name=$m7Bridge]] > 0 || [:len [/container/envs/find where list=$m7Env or list=$gateEnv]] > 0 || [:len [/container/mounts/find where list=$m7Mount]] > 0) do={ :error "Installation name collision" }
:if ([:len [/container/find where comment~"^meridian-install:"]] > 0) do={ :error "Existing Meridian installation found; inspect it before retry" }
:foreach item in=($rootDir,$stateDir,$m7ImagePath,$gateRoot,$gateArchive) do={ :if ([:len [/file/find where name=$item]] > 0) do={ :error "Installation path collision" } }
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
:local requiredBytes 88081920
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
:local gateName ($gc->"gateName")
:local m7Owner ($gc->"owner")
:local m7Mount ($gc->"mount")
:local gateEnv ($gc->"gateEnv")
:local m7Veth ($gc->"veth")
:local gateImageID ($gc->"gateImageID")
:local gateEnvID ($gc->"gateEnvID")
:local gateArchive ($gc->"gateArchive")
:local originalRootID ($gc->"originalRootID")
:local gateRoot ($gc->"gateRoot")
:local deviceID ($gc->"deviceID")
:if ([/container/get $gateID image-id] != $gateImageID || [/container/get $gateID name] != $gateName || [/container/get $gateID comment] != $m7Owner || [:tostr [/container/get $gateID mountlists]] != $m7Mount || [:tostr [/container/get $gateID envlists]] != $gateEnv || [/container/get $gateID interface] != $m7Veth) do={ :error "Verifier ownership changed" }
:onerror alreadyStopped in={ /container/stop $gateID } do={}
:for i from=1 to=40 do={
:local existing [/container/find where name=$gateName and comment=$m7Owner]
:if ([:len $existing] = 0) do={ :break }
:if ([:len $existing] != 1 || [:tostr [:pick $existing 0]] != [:tostr $gateID] || [/container/get $gateID image-id] != $gateImageID) do={ :error "Verifier ownership changed" }
:onerror busy in={ /container/remove $gateID } do={}
:delay 1s
}
:if ([:len [/container/find where name=$gateName]] > 0) do={ :error "Verifier shutdown incomplete" }
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
:local stateDir $2
:local routerIP $3
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
:if ([/container/mounts/get $mountID dst] != "/state" || ($source != ("/" . $stateDir) && $source != $stateDir)) do={ :error "Meridian state mount mismatch" }
:local gate ($stateDir . "/gate-" . $attempt . ".ok")
:local used ($stateDir . "/used-" . $attempt)
:local panel ($stateDir . "/ui-identity")
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 0) || ([:len [/file/find where name=$panel]] != 0)) do={ :error "Meridian installation state already exists" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --authorize-install " . $attempt) no-sh
:if ([:len [/file/find where name=$gate]] != 1) do={ :error "Premium authorization incomplete" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --prepare-panel " . $attempt . " " . [:tostr $routerIP] . " " . [:tostr $panelPort]) no-sh
:if (([:len [/file/find where name=$gate]] != 0) || ([:len [/file/find where name=$used]] != 1)) do={ :error "Premium authorization receipt not consumed" }
:foreach suffix in={"panel.crt";"panel.key";"preview.auth"} do={
:local obj [/file/find where name=($panel . "/" . $suffix)]
:if ([:len $obj] != 1) do={ :error "Panel identity incomplete" }
:if ([/file/get [:pick $obj 0] size] < 1) do={ :error "Panel identity incomplete" }
}
:return true
}
:local netIDs [:toarray ""]
:local ruleHash do={
:local observed $1
:if ([:typeof $observed] != "array" || [:len [:tostr ($observed->".id")]] = 0) do={ :error "Unable to fingerprint network object" }
:foreach field in={"bytes";"packets";"invalid";"hw-offloaded"} do={ :set ($observed->$field) }
:return [:convert [:serialize to=json value=$observed options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local m7PlaceFilter do={
:local ident $1
:local label $2
:local first $3
:if ([/ip/firewall/filter/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/filter/move $ident destination=$first }
/ip/firewall/filter/enable $ident
:return ([/ip/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceFilter6 do={
:local ident $1
:local label $2
:local first $3
:if ([/ipv6/firewall/filter/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ipv6/firewall/filter/move $ident destination=$first }
/ipv6/firewall/filter/enable $ident
:return ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)
}
:local m7PlaceNAT do={
:local ident $1
:local label $2
:local first $3
:if ([/ip/firewall/nat/get $ident comment] != $label) do={ :error "Rule creation uncertain" }
:if ([:typeof $first] = "id") do={ /ip/firewall/nat/move $ident destination=$first }
/ip/firewall/nat/enable $ident
:return ([/ip/firewall/nat/print as-value where .id=$ident]->0)
}
:local modeIDs [:toarray ""]
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
:if (![$m7FetchImage $gateArchive "https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/meridian-hapax2-premium-edaa1c42aadd73aeebe44c8e5bebdb4aeddd5ea97c696186a706ba510b631fe9.tar" 6656000 "5d91af17e1c7293bfe17e7d6cc72bdb46d4fccedafd6a673e0ca46d952ecc6e56f65ebdfb5c493cfd942c2e00ba3e98edbcd8499a9679b4501c3545bd5115c1a" 28006400]) do={ :error "Verifier download failed" }
/interface/bridge/add name=$m7Bridge protocol-mode=none comment=$m7Owner
:set ($gateCtx->"vethID") [/interface/veth/add name=$m7Veth address=([:tostr $m7Backend] . "/30") gateway=$m7Gateway comment=$m7Owner]
/interface/bridge/port/add bridge=$m7Bridge interface=$m7Veth comment=$m7Owner
/ip/address/add address=([:tostr $m7Gateway] . "/30") interface=$m7Bridge comment=$m7Owner
{
:local label ($m7Owner . "-drop-output")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward out-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-drop-input")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-drop-router")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=input in-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-drop6-router")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=input in-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-drop6-from")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward in-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-drop6-to")
:local first [:pick [/ipv6/firewall/filter/find where dynamic=no] 0]
:local ident [/ipv6/firewall/filter/add chain=forward out-interface=$m7Bridge action=drop comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ipv6/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter6 $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-wan-return")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Wan out-interface=$m7Bridge dst-address=$m7Backend connection-state=established,related action=accept comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-wan-out")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Bridge out-interface=$m7Wan src-address=$m7Backend action=accept comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-wan-nat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=srcnat out-interface=$m7Wan src-address=$m7Backend action=masquerade comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
:set ($gateCtx->"mountID") [/container/mounts/add list=$m7Mount src=$stateDir dst=/state]
:local gateEnvID [/container/envs/add list=$gateEnv key=M7_DEVICE_ID value=$deviceID]
/container/add name=$gateName file=$gateArchive root-dir=$gateRoot interface=$m7Veth mountlists=$m7Mount envlists=$gateEnv entrypoint="/usr/local/bin/meridian-installer" logging=no start-on-boot=no comment=$m7Owner
:local gateRows [/container/find where name=$gateName and comment=$m7Owner]
:if ([:len $gateRows] != 1) do={ :error "Verifier container creation uncertain" }
:local gateID [:pick $gateRows 0]
:local gateImageID "f68a45f4585944e13e68f19fdfc8824dd2c94e6d8e1cec9941132a7ef8e6762e"
:if (![$m7WaitImport $gateID $gateImageID]) do={ :error "Verifier import failed" }
:local rootFiles [/file/find where name=$gateRoot]
:local originalRootID
:if ([:len $rootFiles] = 1) do={ :set originalRootID [:pick $rootFiles 0] }
:set ($gateCtx->"gateID") $gateID
:set ($gateCtx->"gateName") $gateName
:set ($gateCtx->"owner") $m7Owner
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
:local bootReady ($stateDir . "/bootstrap-ready")
:for i from=1 to=40 do={ :if ([:len [/file/find where name=$bootReady]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$bootReady]] != 1) do={ :error "Verifier did not start" }
:if (![$authorizePanel $gateName $stateDir $routerIP $m7Port]) do={ :error "Premium authorization failed" }
:if (![$m7RetireGate $gateCtx]) do={ :error "Verifier retirement failed" }
:set ($gateCtx->"retired") true
:if ([:len [/file/find where name=($stateDir . "/ui-identity/preview.auth")]] != 1) do={ :error "Authorized state unavailable" }
:put "Meridian: Premium verified; downloading VPN and panel"
:if (![$m7FetchImage $m7ImagePath "https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/meridian-hapax2-candidate-8f89d1248123b593890672ed082617488490e293c93df327782f32777128b184.tar" 17520640 "b97fd1b150685785126c75a1c5fdd3c70d157fbe65874bc2cfca79d53bf1f47dfbd19b47b31b15aa486b9ef5d2c46133caa0d2af268310844a3e7ada7952ed85" 70561280]) do={ :error "Runtime download failed" }
/container/envs/add list=$m7Env key=M7_INGRESS_IFACE value=$m7Veth
/container/envs/add list=$m7Env key=M7_SOURCE_CIDR value=$clientNetwork
/container/envs/add list=$m7Env key=M7_PANEL_CIDR value=$clientNetwork
/container/envs/add list=$m7Env key=M7_PANEL_BIND value=([:tostr $m7Backend] . ":" . $m7Port)
/container/envs/add list=$m7Env key=M7_PANEL_AUTHORITY value=([:tostr $routerIP] . ":" . $m7Port)
/container/envs/add list=$m7Env key=M7_READY_ID value=$m7Tag
/container/add name=$m7Name file=$m7ImagePath root-dir=$rootDir interface=$m7Veth mountlists=$m7Mount envlists=$m7Env entrypoint="/usr/local/sbin/m7-image-entry" cmd=stack logging=no start-on-boot=no comment=$m7Owner
:set m7C [/container/find where name=$m7Name and comment=$m7Owner]
:if ([:len $m7C] != 1 || ![$m7WaitImport [:pick $m7C 0] "d57f32457912c5a5b61e90e36f978a9277e9e004b310cdc4bd585fd10e728b03"]) do={ :error "VPN container import failed" }
:local stamp ($stateDir . "/stack-ready-" . $m7Tag)
:if ([:len [/file/find where name=$stamp]] != 0) do={ :error "Unexpected startup marker" }
/container/start $m7C
:for i from=1 to=120 do={ :if ([:len [/file/find where name=$stamp]] = 1) do={ :break }; :delay 1s }
:if ([:len [/file/find where name=$stamp]] != 1) do={ :error "VPN tunnel and panel startup unconfirmed" }
{
:local label ($m7Owner . "-panel-reply")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Bridge out-interface=$m7Lan src-address=$m7Backend dst-address=$clientNetwork protocol=tcp src-port=$m7Port connection-state=established connection-nat-state=dstnat action=accept comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-panel-access")
:local first [:pick [/ip/firewall/filter/find where dynamic=no] 0]
:local ident [/ip/firewall/filter/add chain=forward in-interface=$m7Lan out-interface=$m7Bridge src-address=$clientNetwork dst-address=$m7Backend protocol=tcp dst-port=$m7Port connection-nat-state=dstnat action=accept comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/filter/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceFilter $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local label ($m7Owner . "-panel-dnat")
:local first [:pick [/ip/firewall/nat/find where dynamic=no] 0]
:local ident [/ip/firewall/nat/add chain=dstnat in-interface=$m7Lan src-address=$clientNetwork dst-address=$routerIP protocol=tcp dst-port=$m7Port action=dst-nat to-addresses=$m7Backend to-ports=$m7Port comment=$label disabled=yes]
:set ($netIDs->$label) $ident
:set ($netIDs->($label . "-sha")) [$ruleHash ([/ip/firewall/nat/print as-value where .id=$ident]->0)]
:local actual [$m7PlaceNAT $ident $label $first]
:set ($netIDs->($label . "-sha")) [$ruleHash $actual]
}
{
:local fingerprint do={
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
:local layoutPath ($stateDir . "/policy-layout.json")
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
:set ($modeIDs->"table") [/routing/table/add name=$table fib comment=($m7Owner . "-policy-table")]
:set ($modeIDs->"sha-table") [$fingerprint ([/routing/table/print as-value where .id=($modeIDs->"table")]->0)]
:set ($modeIDs->"kill") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table blackhole distance=254 comment=($m7Owner . "-policy-kill")]
:set ($modeIDs->"sha-kill") [$fingerprint ([/ip/route/print as-value where .id=($modeIDs->"kill")]->0)]
:set ($modeIDs->"via") [/ip/route/add dst-address=0.0.0.0/0 routing-table=$table gateway=([:tostr $m7Backend] . "@main") distance=1 comment=($m7Owner . "-policy-via")]
:set ($modeIDs->"sha-via") [$fingerprint ([/ip/route/print as-value where .id=($modeIDs->"via")]->0)]
:set ($modeIDs->"local-black") [/ip/firewall/mangle/add chain=$black dst-address-type=local action=accept comment=($marker . "local-black")]
:set ($modeIDs->"sha-local-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"local-black")]->0)]
:set ($modeIDs->"local-white") [/ip/firewall/mangle/add chain=$white dst-address-type=local action=accept comment=($marker . "local-white")]
:set ($modeIDs->"sha-local-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"local-white")]->0)]
:set ($modeIDs->"keep-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-black")]
:set ($modeIDs->"sha-keep-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"keep-black")]->0)]
:set ($modeIDs->"keep-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7k" . $suffix) action=accept comment=($marker . "keep-white")]
:set ($modeIDs->"sha-keep-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"keep-white")]->0)]
:set ($modeIDs->"listed-black") [/ip/firewall/mangle/add chain=$black dst-address-list=("m7b" . $suffix) action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "listed-black")]
:set ($modeIDs->"sha-listed-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"listed-black")]->0)]
:set ($modeIDs->"listed-white") [/ip/firewall/mangle/add chain=$white dst-address-list=("m7d" . $suffix) action=accept comment=($marker . "listed-white")]
:set ($modeIDs->"sha-listed-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"listed-white")]->0)]
:set ($modeIDs->"default-white") [/ip/firewall/mangle/add chain=$white action=mark-routing new-routing-mark=$table passthrough=no comment=($marker . "default-white")]
:set ($modeIDs->"sha-default-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"default-white")]->0)]
:local guardBlack ("m7gb" . $suffix)
:local guardWhite ("m7gw" . $suffix)
:local replyMark ("m7ri" . $suffix)
:if ([:len [/ip/firewall/filter/find where chain=$guardBlack or chain=$guardWhite]]>0) do={ :error "Policy guard chain collision" }
:set ($modeIDs->"reply-new") [/ip/firewall/mangle/add chain=forward in-interface=$m7Wan out-interface=$m7Lan dst-address=$clientNetwork connection-state=new connection-mark=no-mark action=mark-connection new-connection-mark=$replyMark passthrough=yes disabled=yes comment=($marker . "reply-new")]
:set ($modeIDs->"sha-reply-new") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"reply-new")]->0)]
:set ($modeIDs->"reply-black") [/ip/firewall/mangle/add chain=$black connection-mark=$replyMark action=accept place-before=($modeIDs->"keep-black") comment=($marker . "reply-black")]
:set ($modeIDs->"sha-reply-black") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"reply-black")]->0)]
:set ($modeIDs->"reply-white") [/ip/firewall/mangle/add chain=$white connection-mark=$replyMark action=accept place-before=($modeIDs->"keep-white") comment=($marker . "reply-white")]
:set ($modeIDs->"sha-reply-white") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"reply-white")]->0)]
:set ($modeIDs->"gr-black") [/ip/firewall/filter/add chain=$guardBlack connection-mark=$replyMark action=return comment=($marker . "gr-black")]
:set ($modeIDs->"sha-gr-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gr-black")]->0)]
:set ($modeIDs->"gr-white") [/ip/firewall/filter/add chain=$guardWhite connection-mark=$replyMark action=return comment=($marker . "gr-white")]
:set ($modeIDs->"sha-gr-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gr-white")]->0)]
:set ($modeIDs->"gk-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-black")]
:set ($modeIDs->"sha-gk-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gk-black")]->0)]
:set ($modeIDs->"gk-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7k" . $suffix) action=return comment=($marker . "gk-white")]
:set ($modeIDs->"sha-gk-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gk-white")]->0)]
:set ($modeIDs->"gl-black") [/ip/firewall/filter/add chain=$guardBlack dst-address-list=("m7b" . $suffix) out-interface=("!" . $m7Bridge) action=drop comment=($marker . "gl-black")]
:set ($modeIDs->"sha-gl-black") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gl-black")]->0)]
:set ($modeIDs->"gl-white") [/ip/firewall/filter/add chain=$guardWhite dst-address-list=("m7d" . $suffix) action=return comment=($marker . "gl-white")]
:set ($modeIDs->"sha-gl-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gl-white")]->0)]
:set ($modeIDs->"gd-white") [/ip/firewall/filter/add chain=$guardWhite out-interface=("!" . $m7Bridge) action=drop comment=($marker . "gd-white")]
:set ($modeIDs->"sha-gd-white") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gd-white")]->0)]
:local allForward [/ip/firewall/filter/find where chain=forward]
:set ($modeIDs->"gentry") [/ip/firewall/filter/add chain=forward in-interface=$m7Lan src-address=$clientNetwork action=jump jump-target=$guardBlack disabled=yes place-before=[:pick $allForward 0] comment=($marker . "gentry")]
:set ($modeIDs->"sha-gentry") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"gentry")]->0)]
:set ($modeIDs->"lan-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$clientNetwork comment=($m7Owner . "-policy-keep-lan")]
:set ($modeIDs->"sha-lan-keep") [$fingerprint ([/ip/firewall/address-list/print as-value where .id=($modeIDs->"lan-keep")]->0)]
:local linkAddr [/ip/address/find where interface=$m7Bridge and comment=$m7Owner]
:if ([:len $linkAddr] != 1) do={ :error "VPN link address unavailable" }
:local linkNetwork [:tostr [/ip/address/get [:pick $linkAddr 0] network]]
:set ($modeIDs->"link-keep") [/ip/firewall/address-list/add list=("m7k" . $suffix) address=($linkNetwork . "/30") comment=($m7Owner . "-policy-keep-link")]
:set ($modeIDs->"sha-link-keep") [$fingerprint ([/ip/firewall/address-list/print as-value where .id=($modeIDs->"link-keep")]->0)]
:local n 0
:foreach aid in=[/ip/address/find where disabled=no and invalid=no] do={
:local iface [:tostr [/ip/address/get $aid interface]]
:if ($iface!=[:tostr $m7Wan] && $iface!=[:tostr $m7Bridge] && $iface!=[:tostr $m7Lan]) do={
:local a [:tostr [/ip/address/get $aid address]]
:local slash [:find $a "/"]
:if ([:typeof $slash]!="num") do={ :error "Incomplete connected subnet" }
:local bits [:tonum [:pick $a ($slash+1) [:len $a]]]
:if ($bits<8 || $bits>32 || $n>=128) do={ :error "Unsupported connected subnet" }
:local prefix ([:tostr [/ip/address/get $aid network]] . "/" . $bits)
:local keepName ("m7k" . $suffix)
:if ([:len [/ip/firewall/address-list/find where list=$keepName and address=$prefix]]>0) do={ :continue }
:local key ("connected-" . $n)
:local item [/ip/firewall/address-list/add list=("m7k" . $suffix) address=$prefix comment=($m7Owner . "-" . $key)]
:set ($modeIDs->$key) $item
:set ($modeIDs->("sha-" . $key)) [$fingerprint ([/ip/firewall/address-list/print as-value where .id=$item]->0)]
:set n ($n+1)
}
}
:set ($modeIDs->"entry") [/ip/firewall/mangle/add chain=prerouting in-interface=$m7Lan src-address=$clientNetwork action=jump jump-target=$black disabled=yes comment=($marker . "entry")]
:set ($modeIDs->"sha-entry") [$fingerprint ([/ip/firewall/mangle/print as-value where .id=($modeIDs->"entry")]->0)]
:local denyOut [/ip/firewall/filter/find where comment=($m7Owner . "-drop-output")]
:local denyIn [/ip/firewall/filter/find where comment=($m7Owner . "-drop-input")]
:if ([:len $denyOut] != 1 || [:len $denyIn] != 1) do={ :error "Missing container boundary" }
:set ($modeIDs->"vpn-out") [/ip/firewall/filter/add chain=forward in-interface=$m7Lan out-interface=$m7Bridge src-address=$clientNetwork dst-address=("!" . [:tostr $m7Backend]) routing-mark=$table action=accept place-before=[:pick $denyOut 0] disabled=yes comment=($m7Owner . "-policy-vpn-out")]
:set ($modeIDs->"sha-vpn-out") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"vpn-out")]->0)]
:set ($modeIDs->"vpn-in") [/ip/firewall/filter/add chain=forward in-interface=$m7Bridge out-interface=$m7Lan src-address=("!" . [:tostr $m7Backend]) dst-address=$clientNetwork connection-state=established,related action=accept place-before=[:pick $denyIn 0] disabled=yes comment=($m7Owner . "-policy-vpn-in")]
:set ($modeIDs->"sha-vpn-in") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"vpn-in")]->0)]
:set ($modeIDs->"panel-reject") [/ip/firewall/filter/add chain=forward in-interface=$m7Bridge out-interface=$m7Lan src-address=$m7Backend dst-address=$clientNetwork protocol=tcp src-port=$m7Port action=drop place-before=[:pick $denyIn 0] disabled=yes comment=($m7Owner . "-policy-panel-reject")]
:set ($modeIDs->"sha-panel-reject") [$fingerprint ([/ip/firewall/filter/print as-value where .id=($modeIDs->"panel-reject")]->0)]
:set ($modeIDs->"panel-out") [/ip/firewall/raw/add chain=prerouting in-interface=$m7Lan src-address=$clientNetwork dst-address=$routerIP protocol=tcp dst-port=$m7Port action=accept disabled=yes comment=($m7Owner . "-policy-panel-out")]
:set ($modeIDs->"sha-panel-out") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($modeIDs->"panel-out")]->0)]
:set ($modeIDs->"panel-in") [/ip/firewall/raw/add chain=prerouting in-interface=$m7Bridge src-address=$m7Backend dst-address=$clientNetwork protocol=tcp src-port=$m7Port action=accept disabled=yes comment=($m7Owner . "-policy-panel-in")]
:set ($modeIDs->"sha-panel-in") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($modeIDs->"panel-in")]->0)]
:set ($modeIDs->"router-in") [/ip/firewall/raw/add chain=prerouting in-interface=$m7Lan src-address=$clientNetwork dst-address=$routerIP action=accept disabled=yes comment=($m7Owner . "-policy-router-in")]
:set ($modeIDs->"sha-router-in") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($modeIDs->"router-in")]->0)]
:set ($modeIDs->"out4") [/ip/firewall/raw/add chain=prerouting src-address=$clientNetwork action=drop disabled=yes comment=($marker . "out4")]
:set ($modeIDs->"sha-out4") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($modeIDs->"out4")]->0)]
:set ($modeIDs->"in4") [/ip/firewall/raw/add chain=prerouting dst-address=$clientNetwork action=drop disabled=yes comment=($marker . "in4")]
:set ($modeIDs->"sha-in4") [$fingerprint ([/ip/firewall/raw/print as-value where .id=($modeIDs->"in4")]->0)]
:if ($count6 = 1) do={
:set ($modeIDs->"out6") [/ipv6/firewall/raw/add chain=prerouting src-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "out6")]
:set ($modeIDs->"sha-out6") [$fingerprint ([/ipv6/firewall/raw/print as-value where .id=($modeIDs->"out6")]->0)]
:set ($modeIDs->"in6") [/ipv6/firewall/raw/add chain=prerouting dst-address=$m7ModeV6 action=drop disabled=yes comment=($marker . "in6")]
:set ($modeIDs->"sha-in6") [$fingerprint ([/ipv6/firewall/raw/print as-value where .id=($modeIDs->"in6")]->0)]
}
:set ($modeIDs->"v6-from") [/ipv6/firewall/filter/add chain=forward in-interface=$m7Lan action=drop disabled=yes comment=($m7Owner . "-policy-v6-from")]
:set ($modeIDs->"sha-v6-from") [$fingerprint ([/ipv6/firewall/filter/print as-value where .id=($modeIDs->"v6-from")]->0)]
:set ($modeIDs->"v6-to") [/ipv6/firewall/filter/add chain=forward out-interface=$m7Lan action=drop disabled=yes comment=($m7Owner . "-policy-v6-to")]
:set ($modeIDs->"sha-v6-to") [$fingerprint ([/ipv6/firewall/filter/print as-value where .id=($modeIDs->"v6-to")]->0)]
:local layout [:toarray ""]
:set ($layout->"version") 1
:set ($layout->"phase") "staged"
:set ($layout->"tag") $m7Tag
:set ($layout->"mode_tag") $suffix
:set ($layout->"source") $clientNetwork
:set ($layout->"ipv6") [:tostr $m7ModeV6]
:set ($layout->"lan") $m7Lan
:set ($layout->"link") $m7Bridge
:set ($layout->"veth") $m7Veth
:set ($layout->"wan") $m7Wan
:set ($layout->"backend") [:tostr $m7Backend]
:set ($layout->"router") [:tostr $routerIP]
:set ($layout->"port") $m7Port
:local refs [:toarray ""]
:foreach k,id in=$modeIDs do={ :if ([:pick $k 0 4] != "sha-") do={ :set ($refs->$k) [:tostr $id] } }
:set ($layout->"objects") $refs
:local body [:serialize to=json value=$layout options=json.no-string-conversion]
/file/add name=$layoutPath type=file contents=$body
:local created [/file/find where name=$layoutPath]
:if ([:len $created] != 1 || [/file/get [:pick $created 0] contents] != $body) do={ :error "Policy layout not persisted" }
:set ($modeIDs->"layout") [:pick $created 0]
:set ($modeIDs->"digest") [:convert $body from=raw transform=sha512 to=hex]
:local hostLayout ($flash . "m7h" . $suffix . ".json")
:local hostState ($flash . "m7x" . $suffix . ".json")
:local hostCtl ("m7ctl" . $suffix)
:if ([:len [/file/find where name=$hostLayout or name=$hostState]]>0 || [:len [/system/script/find where name=$hostCtl]]>0 || [:len [/system/scheduler/find where name=$hostCtl]]>0) do={ :error "Policy controller ownership collision" }
/file/add name=$hostLayout type=file contents=$body
:set ($modeIDs->"host-layout") [:pick [/file/find where name=$hostLayout] 0]
:local initial "{\"phase\":\"stable\",\"mode\":\"off\",\"digest\":\"\",\"list\":\"\",\"last\":\"\"}"
/file/add name=$hostState type=file contents=$initial
:set ($modeIDs->"host-state") [:pick [/file/find where name=$hostState] 0]
:set ($modeIDs->"initial") $initial
:local fetched [/tool/fetch url="https://raw.githubusercontent.com/JinComputers/meridian-linux/01d164f7359b71d05721ec7afa821b5441d999b4/native-mode-dispatch.rsc" output=user as-value check-certificate=yes http-max-redirect-count=0 idle-timeout=30s]
:local program ($fetched->"data")
:if (($fetched->"status")!="finished" || [:convert $program from=raw transform=sha512 to=hex]!="2715b8cafa82548e66b20dee25afeb1360fa38c20ca3dd173b511a5fa9f5644176e75b8963e774cb8e730baaeb1ed6773b56a97a6301038ff5fd7d0b68bd364b") do={ :error "Policy controller checksum mismatch" }
:set ($modeIDs->"script") [/system/script/add name=$hostCtl source=$program policy=ftp,read,write,test,sensitive comment=($m7Owner . "-policy-controller")]
:set ($modeIDs->"scheduler") [/system/scheduler/add name=$hostCtl interval=10s start-date=[/system/clock/get date] start-time=[/system/clock/get time] on-event=$hostCtl policy=ftp,read,write,test,sensitive comment=($m7Owner . "-policy-controller")]
:put ("M7POLICY staged source=" . $clientNetwork . " link=" . $m7Bridge)
}
:local loginStamp ($stateDir . "/panel-login-" . $m7Tag)
:if ([:len [/file/find where name=$loginStamp]] != 0) do={ :error "Unexpected login marker" }
/container/shell $m7C cmd=("/usr/local/bin/meridian-installer --panel-login " . [:tostr $routerIP] . " " . [:tostr $m7Port]) no-sh
:if ([:len [/file/find where name=$loginStamp]] != 1) do={ :error "Panel login was not delivered" }
/container/set $m7C start-on-boot=yes
$releaseLock $lockID $m7Tag $lockName
:put ("M7INSTALL stack-ready name=" . $m7Name . " panel=https://" . [:tostr $routerIP] . ":" . $m7Port . "/")
} do={
:put ("M7INSTALL interrupted: " . $failure)
:if ([:typeof $lockID] != "id") do={ :error "Meridian installer lock unavailable" }
:local attached [/interface/bridge/port/find where interface=$m7Veth and bridge=$m7Bridge and comment=$m7Owner]
:if ([:len $attached] = 1) do={ /interface/bridge/port/disable $attached }
:foreach objectName in=($gateName,$m7Name) do={
:local current [/container/find where name=$objectName and comment=$m7Owner]
:if ([:len $current] = 1) do={ :onerror e in={ /container/set $current start-on-boot=no; /container/stop $current } do={} }
}
{
:local fingerprint do={
:local row $1
:if ([:typeof $row] != "array" || [:len [:tostr ($row->".id")]] = 0) do={ :error "Policy rule readback failed" }
:foreach field in={"bytes";"packets";"active";"invalid";"immediate-gw";"inactive";"hw-offloaded"} do={ :set ($row->$field) }
:return [:convert [:serialize to=json value=$row options=json.no-string-conversion] from=raw transform=sha512 to=hex]
}
:local hostState ($modeIDs->"host-state")
:if ([:typeof $hostState]="id") do={
:if ([/file/get $hostState contents]!=($modeIDs->"initial")) do={ :error "Active host policy requires reversal before cleanup" }
}
:local worker ("m7ctl" . $suffix)
:local schedulerID ($modeIDs->"scheduler")
:if ([:typeof $schedulerID]="id") do={
:if ([/system/scheduler/get $schedulerID comment]!=($m7Owner . "-policy-controller") || [/system/scheduler/get $schedulerID name]!=$worker || [:tostr [/system/scheduler/get $schedulerID on-event]]!=$worker) do={ :error "Scheduler ownership changed" }
/system/scheduler/remove $schedulerID
}
:local workerID ($modeIDs->"script")
:if ([:typeof $workerID]="id") do={
:if ([:len [/system/script/job/find where script=$worker]]>0 || [/system/script/get $workerID comment]!=($m7Owner . "-policy-controller") || [/system/script/get $workerID name]!=$worker) do={ :error "Policy worker is busy or changed" }
/system/script/remove $workerID
}
:local hostConfig ($modeIDs->"host-layout")
:if ([:typeof $hostConfig]="id") do={
:if ([:convert [/file/get $hostConfig contents] from=raw transform=sha512 to=hex]!=($modeIDs->"digest")) do={ :error "Host policy layout changed" }
/file/remove $hostConfig
}
:if ([:typeof $hostState]="id") do={
/file/remove $hostState
}
:local f ($modeIDs->"layout")
:if ([:typeof $f] = "id") do={
:local files [/file/find where name=($stateDir . "/policy-layout.json")]
:if ([:len $files] != 1 || [:tostr [:pick $files 0]] != [:tostr $f]) do={ :error "Policy layout ownership changed" }
:if ([:convert [/file/get $f contents] from=raw transform=sha512 to=hex] != ($modeIDs->"digest")) do={ :error "Policy layout changed" }
/file/remove $f
}
:foreach role in={"v6-to";"v6-from"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/filter/remove $id
}
}
:foreach role in={"in6";"out6"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ipv6/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ipv6/firewall/raw/remove $id
}
}
:foreach role in={"in4";"out4";"router-in";"panel-in";"panel-out"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/raw/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/raw/remove $id
}
}
:foreach role in={"gentry";"gd-white";"gl-white";"gl-black";"gk-white";"gk-black";"gr-white";"gr-black";"panel-reject";"vpn-in";"vpn-out"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/filter/remove $id
}
}
:foreach role in={"entry";"reply-white";"reply-black";"reply-new";"default-white";"listed-white";"listed-black";"keep-white";"keep-black";"local-white";"local-black"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/mangle/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/mangle/remove $id
}
}
:foreach role,id in=$modeIDs do={
:if ([:pick $role 0 10]="connected-" && [:typeof $id]="id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows]!=1 || [$fingerprint ($rows->0)]!=($modeIDs->("sha-" . $role))) do={ :error "Connected network ownership changed" }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"link-keep";"lan-keep"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/firewall/address-list/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/firewall/address-list/remove $id
}
}
:foreach role in={"via";"kill"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/ip/route/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/ip/route/remove $id
}
}
:foreach role in={"table"} do={
:local id ($modeIDs->$role)
:if ([:typeof $id] = "id") do={
:local rows [/routing/table/print as-value where .id=$id]
:if ([:len $rows] != 1 || [:tostr (($rows->0)->".id")] != [:tostr $id] || [$fingerprint ($rows->0)] != ($modeIDs->("sha-" . $role))) do={ :error ("Policy recovery requires inspection: " . $role) }
/routing/table/remove $id
}
}
}
:foreach part in={"panel-dnat";"wan-nat"} do={
:local label ($m7Owner . "-" . $part)
:local ident ($netIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/nat/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$ruleHash ($rows->0)] != ($netIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/nat/remove $ident
}
}
:foreach part in={"panel-access";"panel-reply";"wan-out";"wan-return";"drop-router";"drop-input";"drop-output"} do={
:local label ($m7Owner . "-" . $part)
:local ident ($netIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ip/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$ruleHash ($rows->0)] != ($netIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ip/firewall/filter/remove $ident
}
}
:foreach part in={"drop6-to";"drop6-from";"drop6-router"} do={
:local label ($m7Owner . "-" . $part)
:local ident ($netIDs->$label)
:if ([:typeof $ident] = "id") do={
:local rows [/ipv6/firewall/filter/print as-value where .id=$ident]
:if ([:len $rows] != 1 || (($rows->0)->"comment") != $label || [$ruleHash ($rows->0)] != ($netIDs->($label . "-sha"))) do={ :error "Owned firewall rule changed; recovery required" }
/ipv6/firewall/filter/remove $ident
}
}
:local ownedIP [/ip/address/find where interface=$m7Bridge and comment=$m7Owner and address=([:tostr $m7Gateway] . "/30")]
:if ([:len $ownedIP] = 1) do={ /ip/address/remove $ownedIP }
:if ([:len $attached] = 1 && [/interface/bridge/port/get $attached comment] = $m7Owner && [/interface/bridge/port/get $attached bridge] = $m7Bridge && [/interface/bridge/port/get $attached interface] = $m7Veth) do={ /interface/bridge/port/remove $attached }
:local createdBridge [/interface/bridge/find where name=$m7Bridge and comment=$m7Owner]
:if ([:len $createdBridge] = 1 && [:len [/interface/bridge/port/find where bridge=$m7Bridge]] = 0 && [:len [/ip/address/find where interface=$m7Bridge]] = 0 && [:len [/ipv6/address/find where interface=$m7Bridge and dynamic=no]] = 0) do={ /interface/bridge/remove $createdBridge }
:local knownGate ($gateCtx->"gateID")
:if ([:typeof $knownGate] = "id" && ($gateCtx->"retired") != true && [:len [/file/find where name=($stateDir . "/key") or name=($stateDir . "/ui-identity")]] = 0) do={
:if (![$m7RetireGate $gateCtx]) do={ :error "Verifier recovery required" }
:local oldMount [/container/mounts/find where list=$m7Mount and dst="/state"]
:local oldLink [/interface/veth/find where name=$m7Veth and comment=$m7Owner]
:if ([:len $oldMount] != 1 || [:tostr [:pick $oldMount 0]] != [:tostr ($gateCtx->"mountID")] || [:len $oldLink] != 1 || [:tostr [:pick $oldLink 0]] != [:tostr ($gateCtx->"vethID")] || [:len [/container/find where interface=$m7Veth]] > 0) do={ :error "Verifier mount or VETH ownership changed" }
:foreach other in=[/container/find] do={
:local lists ("," . [:tostr [/container/get $other mountlists]] . ",")
:if ([:typeof [:find $lists ("," . $m7Mount . ",")]] = "num") do={ :error "Verifier mount still in use" }
}
/container/mounts/remove $oldMount
/interface/veth/remove $oldLink
:local clean true
:local prefix ($stateDir . "/")
:foreach member in=[/file/find] do={
:local pathName [/file/get $member name]
:if ([:pick $pathName 0 [:len $prefix]] = $prefix && $pathName != ($prefix . "bootstrap-ready") && $pathName != ($prefix . "device-id")) do={ :set clean false }
}
:local saved [/file/find where name=($prefix . "device-id")]
:if ($clean && [:len $saved] = 1 && [/file/get $saved contents] = ($deviceID . "\n")) do={
:local sd [/file/find where name=$stateDir]
:if ([:len $sd] = 1 && [/file/get $sd type] = "directory") do={ /file/remove $sd }
}
$releaseLock $lockID $m7Tag $lockName
:put "M7INSTALL verifier removed; network restored; retry is available"
:error "Meridian installation incomplete"
}
$releaseLock $lockID $m7Tag $lockName
:put ("M7INSTALL network rules reverted; retained installation " . $m7Name . " gate " . $gateName . " state " . $stateDir . " archive " . $m7ImagePath)
:error "Meridian installation incomplete"
}
}
MERIDIAN_ROUTEROS_RSC
}

main() {
	case "${1:-}" in
        --routeros-script) meridian_routeros_script; return ;;
        --routeros-install-command)
            cat <<'MERIDIAN_ROUTEROS_COMMAND'
{ :onerror e in={ :local r [/tool/fetch url="http://45.10.247.23/bin/m.sh" output=user as-value http-max-redirect-count=0 idle-timeout=30s]; :if (($r->"status")!="finished") do={ :error "download failed" }; :local body ($r->"data"); :local begin "cat <<'MERIDIAN_ROUTEROS_RSC'\n"; :local p [:find $body $begin]; :if ([:typeof $p]!="num") do={ :error "RouterOS installer absent" }; :set p ($p+[:len $begin]); :local q [:find $body "\nMERIDIAN_ROUTEROS_RSC\n" $p]; :if ([:typeof $q]!="num") do={ :error "incomplete RouterOS installer" }; :local s [:pick $body $p $q]; :if ([:convert $s from=raw transform=sha512 to=hex]!="4e8a75e763ed5decb8a5abccc04b886728692d46d1262f834399f1045f2ef56fe2f3e50ce09966e210619e18f657f1346aae9fe5ca3401ca4a391cabfe7970ff") do={ :error "installer checksum mismatch" }; :local run [:parse $s]; $run } do={ :put ("Meridian: " . $e) } }
MERIDIAN_ROUTEROS_COMMAND
            return ;;
    esac
	F=/tmp/install.sh
	S=/tmp/install.sh.sums
	skachat() {  # $1 куда, $2 откуда
		rm -f "$1"
		if command -v curl >/dev/null 2>&1; then
			curl -fsSL --connect-timeout 10 --max-time 300 -o "$1" "$2" 2>/dev/null && [ -s "$1" ] && return 0
		fi
		wget -q -T 30 -O "$1" "$2" 2>/dev/null && [ -s "$1" ] && return 0
		wget -q -O "$1" "$2" 2>/dev/null && [ -s "$1" ] && return 0
		rm -f "$1"
		return 1
	}
	imya() {
		case "$1" in
			*45.10.247.23*) echo "зеркало RU" ;;
			*githubusercontent*) echo "GitHub" ;;
			*) echo "сервер" ;;
		esac
	}
	OTKUDA=""
	for B in http://45.10.247.23/bin \
		https://raw.githubusercontent.com/JinComputers/meridian-router/entware \
		http://138.124.78.252:8080/bin; do
		echo "[meridian] Подключаюсь к источнику: $(imya "$B")… скачиваю установщик…"
		skachat "$F" "$B/install.sh" || { echo "[meridian]   источник $(imya "$B") не ответил — пробую следующий"; continue; }
		head -1 "$F" | grep -q '^#!' || { echo "[meridian] $B: пришёл не скрипт — пробую другой источник"; continue; }
		if command -v sha256sum >/dev/null 2>&1 && skachat "$S" "$B/SHA256SUMS"; then
			NADO=$(awk '$2 == "install.sh" {print $1}' "$S")
			EST=$(sha256sum "$F" | cut -d' ' -f1)
			if [ -n "$NADO" ] && [ "$NADO" != "$EST" ]; then
				echo "[meridian] $B: установщик не сошёлся с SHA256SUMS — пробую другой источник"
				continue
			fi
		fi
		OTKUDA=$B
		break
	done
	rm -f "$S"
	if [ -z "$OTKUDA" ]; then
		rm -f "$F"
		echo "[meridian] Не удалось скачать установщик ни из одного источника. Проверьте, есть ли на роутере интернет, и повторите."
		exit 1
	fi
	echo "[meridian] установщик скачан ($(imya "$OTKUDA"))"
	# Только для проверки загрузчика: скачать и сверить, не запуская.
	[ -n "${M_TOLKO_SKACHAT:-}" ] && exit 0
	if [ -t 0 ]; then
		exec sh "$F" "$@"
	fi
	if (: < /dev/tty) 2>/dev/null; then
		exec sh "$F" "$@" < /dev/tty
	fi
	exec sh "$F" "$@"
}

main "$@"
