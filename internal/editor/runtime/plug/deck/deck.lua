VERSION = "1.0.0"

local micro = import("micro")
local buffer = import("micro/buffer")

local stateFile = os.getenv("DECK_EDITOR_STATE") or ""
local wantTop = tonumber(os.getenv("DECK_EDITOR_TOP") or "") or 0
local saved = {}
local position = nil
local written = nil

local function topLine(bp)
    local start = bp:GetView().StartLine
    if type(start) == "number" then return start end
    return start.Line
end

local function snapshot(bp)
    if bp == nil or bp.Buf == nil or bp.Buf.Type.Kind ~= buffer.BTDefault then return end
    local path = bp.Buf.AbsPath
    if path == nil or path == "" then return end
    local c = bp.Buf:GetActiveCursor()
    position = string.format("%s\n%d %d %d\n", path, c.Y + 1, c.X + 1, topLine(bp) + 1)
end

local function flush()
    if position == nil then return end
    local content = position .. table.concat(saved, "")
    if content == written then return end
    local f = io.open(stateFile, "w")
    if f == nil then return end
    f:write(content)
    f:close()
    written = content
end

local function record(bp)
    if stateFile == "" then return end
    snapshot(bp or micro.CurPane())
    flush()
end

local function restoreTop(bp)
    if wantTop <= 0 or bp == nil or bp:GetView().Height <= 0 then return end
    local delta = (wantTop - 1) - topLine(bp)
    if delta > 0 then
        bp:ScrollDown(delta)
    elseif delta < 0 then
        bp:ScrollUp(-delta)
    end
    if topLine(bp) ~= wantTop - 1 then bp:Center() end
    wantTop = 0
end

function postinit()
    if stateFile == "" then return end
    record()
end

function onAnyEvent()
    if stateFile == "" then return end
    restoreTop(micro.CurPane())
    record()
end

function preQuit(bp)
    record(bp)
    return true
end

function preQuitAll(bp)
    record(bp)
    return true
end

function onSave(bp)
    if stateFile == "" or bp == nil or bp.Buf == nil then return end
    local line = bp.Buf.AbsPath .. "\n"
    for _, s in ipairs(saved) do
        if s == line then
            record(bp)
            return
        end
    end
    table.insert(saved, line)
    record(bp)
end

function gotoAtMouse(bp, e)
    if e ~= nil then bp:MousePress(e) end
    bp:HandleCommand("lsp goto-definition")
    return true
end
