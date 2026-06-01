#!/usr/bin/env python3
"""Sync plan/tasks.yaml -> GitHub milestones, labels, issues. Idempotent + lazy.

LAZY MODEL (issue states):
  OPEN, no `blocked` label  -> actionable now
  CLOSED + `blocked` label  -> backlog, waiting on deps (hidden from default open list)
  CLOSED, no `blocked` label -> done (closed via PR / completion)

A dependency counts as satisfied only when its issue is CLOSED and NOT `blocked`.
Tasks with unmet deps are created as backlog (closed+blocked). When you complete a task
(close its issue), the Unblock Action re-runs this script and REOPENS any task whose deps
are now all done — so issues surface on their own. Full backlog always lives in tasks.yaml
and ROADMAP.md. (GitHub org policy blocks issue deletion, hence closed+blocked instead of
delete; behavior is equivalent — the open list only ever shows actionable work.)

Usage:
  python3 plan/sync_issues.py --repo 4rtdr0p/artdrop-protocol
  python3 plan/sync_issues.py --repo 4rtdr0p/artdrop-protocol --roadmap-only
  python3 plan/sync_issues.py --repo 4rtdr0p/artdrop-protocol --dry-run

Matches existing issues by the "[ID]" title prefix, so re-running never duplicates.
Requires: gh (authenticated), pyyaml.
"""
import argparse, json, os, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
TASKS = os.path.join(HERE, "tasks.yaml")
ROADMAP = os.path.join(os.path.dirname(HERE), "ROADMAP.md")

DOMAIN_COLORS = {
    "research": "5319e7", "core": "1d76db", "implementation": "0e8a16",
    "certificate": "fbca04", "escrow": "d93f0b", "wallet-api": "b60205",
    "yield": "0052cc", "loyalty": "c2e0c6", "compliance": "5319e7",
    "governance": "e99695", "events": "bfdadc", "infra": "555555", "deploy": "000000",
}
OWNER_COLOR = "ededed"

# owner label -> GitHub logins to assign. 'rey' is intentionally unmapped (not on GitHub).
ASSIGNEES = {
    "claudio": ["claucondor"],
    "ariel-edgar": ["a-david-dev", "Edgar666-debug"],
    "team": ["claucondor"],
}


def assignees_for(t):
    out = []
    for o in t.get("owner", []):
        for u in ASSIGNEES.get(o, []):
            if u not in out:
                out.append(u)
    return out


def sh(args, check=True, capture=True):
    r = subprocess.run(args, capture_output=capture, text=True)
    if check and r.returncode != 0:
        sys.stderr.write((r.stderr or r.stdout or "") + "\n")
        raise SystemExit(f"command failed: {' '.join(args)}")
    return r


def load():
    import yaml
    with open(TASKS) as f:
        return yaml.safe_load(f)


def gh_api(repo, path, method="GET", fields=None):
    args = ["gh", "api", f"repos/{repo}/{path}", "-X", method]
    for k, v in (fields or {}).items():
        args += ["-f", f"{k}={v}"]
    return sh(args)


def ensure_milestones(repo, phases, dry):
    existing = json.loads(sh(["gh", "api", f"repos/{repo}/milestones?state=all&per_page=100"]).stdout)
    by_title = {m["title"]: m["number"] for m in existing}
    out = {}
    for key, title in phases.items():
        if title in by_title:
            out[key] = by_title[title]
            continue
        print(f"+ milestone {title}")
        if dry:
            out[key] = -1
            continue
        r = gh_api(repo, "milestones", "POST", {"title": title})
        out[key] = json.loads(r.stdout)["number"]
    return out


def ensure_labels(repo, tasks, dry):
    existing = {l["name"] for l in json.loads(sh(["gh", "api", f"repos/{repo}/labels?per_page=100"]).stdout)}
    wanted = {"blocked": "d73a4a"}
    for t in tasks:
        wanted[t["domain"]] = DOMAIN_COLORS.get(t["domain"], "cccccc")
        for o in t.get("owner", []):
            wanted[f"owner:{o}"] = OWNER_COLOR
    for name, color in sorted(wanted.items()):
        if name in existing:
            continue
        print(f"+ label {name}")
        if dry:
            continue
        sh(["gh", "label", "create", name, "--repo", repo, "--color", color, "--force"])


def body_for(t):
    def fmt(i):
        if isinstance(i, dict):
            return "; ".join(f"{k}: {v}" for k, v in i.items())
        return str(i).strip()

    def block(title, items):
        if not items:
            return ""
        lines = "\n".join(f"- {fmt(i)}" for i in items)
        return f"\n### {title}\n{lines}\n"
    deps = ", ".join(t.get("depends_on", [])) or "—"
    ref = t.get("ref", "—")
    s = f"**ID** `{t['id']}` · **Repo** `{t.get('repo','artdrop-protocol')}` · **Ref** {ref} · **Depends on** {deps}\n"
    if t.get("context"):
        s += f"\n{t['context'].strip()}\n"
    s += block("Steps", t.get("steps"))
    s += block("Validate (PR checklist)", t.get("validate"))
    s += block("Risks", t.get("risks"))
    if t.get("why"):
        s += f"\n### Why\n{t['why'].strip()}\n"
    s += f"\n<sub>Generated from `plan/tasks.yaml`. Edit there + here on scope change.</sub>\n"
    return s


def short(repo):
    return repo.split("/")[-1]


def trepo(t):
    return t.get("repo", "artdrop-protocol")


def read_repo_issues(full_repo):
    """id -> {'number','state','labels'} for plan issues ([ID] prefix) in one repo."""
    r = sh(["gh", "issue", "list", "--repo", full_repo, "--state", "all",
            "--limit", "500", "--json", "title,number,state,labels"], check=False)
    if r.returncode != 0 or not r.stdout.strip():
        return {}
    out = {}
    for row in json.loads(r.stdout):
        title = row["title"]
        if title.startswith("[") and "]" in title:
            out[title[1:title.index("]")]] = {
                "number": row["number"],
                "state": row["state"].upper(),
                "labels": {l["name"] for l in row.get("labels", [])},
            }
    return out


def _done(dep, gstate):
    """A dependency is satisfied only when its issue is closed AND not 'blocked'.
    State is read from the dependency's OWN repo (cross-repo aware)."""
    v = gstate.get(dep)
    return bool(v) and v["state"] == "CLOSED" and "blocked" not in v["labels"]


def sync_issues(home_full, data, dry):
    """States: OPEN(no blocked)=actionable · CLOSED+blocked=backlog · CLOSED(no blocked)=done.
    Only manages tasks whose repo == home repo; dep state is read across all plan repos.
    create-blocked / unblock(reopen) / demote(initial migration) — all idempotent."""
    org, home = home_full.split("/")[0], short(home_full)
    repos = sorted({trepo(t) for t in data["tasks"]})
    repo_issues = {r: read_repo_issues(f"{org}/{r}") for r in repos}
    gstate = {}
    for t in data["tasks"]:
        v = repo_issues[trepo(t)].get(t["id"])
        if v:
            gstate[t["id"]] = v
    have = repo_issues.get(home, {})  # issues physically in the home repo
    repo = home_full
    n_new = n_unblocked = n_demoted = n_skip = 0
    for t in data["tasks"]:
        if trepo(t) != home:
            n_skip += 1
            continue  # belongs to another repo — that repo's sync run owns it
        tid = t["id"]
        ok = all(_done(d, gstate) for d in t.get("depends_on", []))
        v = have.get(tid)
        if v is None:
            title = f"[{tid}] {t['title']}"
            labels = [t["domain"]] + [f"owner:{o}" for o in t.get("owner", [])]
            if not ok:
                labels.append("blocked")
            ms = data["phases"][t["phase"]]
            print(f"+ {'issue' if ok else 'backlog'} {title}")
            n_new += 1
            if dry:
                continue
            args = ["gh", "issue", "create", "--repo", repo, "--title", title,
                    "--body", body_for(t), "--milestone", ms]
            for l in labels:
                args += ["--label", l]
            url = sh(args).stdout.strip()
            asg = assignees_for(t)
            if asg:  # tolerate non-collaborators across repos
                sh(["gh", "issue", "edit", url, "--add-assignee", ",".join(asg)], check=False)
            if not ok:
                sh(["gh", "issue", "close", url, "--repo", repo,
                    "--reason", "not planned"])
        elif v["state"] == "CLOSED" and "blocked" in v["labels"] and ok:
            print(f"^ unblock {tid} (#{v['number']})")
            n_unblocked += 1
            if dry:
                continue
            sh(["gh", "issue", "edit", str(v["number"]), "--repo", repo, "--remove-label", "blocked"])
            asg = assignees_for(t)
            if asg:
                sh(["gh", "issue", "edit", str(v["number"]), "--repo", repo,
                    "--add-assignee", ",".join(asg)])
            sh(["gh", "issue", "reopen", str(v["number"]), "--repo", repo])
        elif v["state"] == "OPEN" and not ok and "blocked" not in v["labels"]:
            print(f"v demote {tid} (#{v['number']}) -> backlog")
            n_demoted += 1
            if dry:
                continue
            sh(["gh", "issue", "edit", str(v["number"]), "--repo", repo, "--add-label", "blocked"])
            sh(["gh", "issue", "close", str(v["number"]), "--repo", repo, "--reason", "not planned"])
    print(f"[{home}] new={n_new} unblocked={n_unblocked} demoted={n_demoted} "
          f"existing={len(have)} skipped(other-repo)={n_skip}")


def write_roadmap(data):
    lines = ["# Roadmap\n",
             "Master plan for the whole product. Issues are **federated per repo** (`Repo` column) "
             "and **lazy-created** (a task becomes an issue only once its deps are closed). This is "
             "the full backlog; not all rows are open issues yet. Unified board: the org GitHub "
             "Project *ArtDrop V2* aggregates issues across repos.\n"]
    for key, ptitle in data["phases"].items():
        lines.append(f"\n## {ptitle}\n")
        lines.append("| ID | Task | Repo | Owner | Depends on |")
        lines.append("|----|------|------|-------|------------|")
        for t in data["tasks"]:
            if t["phase"] != key:
                continue
            owners = ", ".join(t.get("owner", [])) or "—"
            deps = ", ".join(t.get("depends_on", [])) or "—"
            lines.append(f"| `{t['id']}` | {t['title']} | `{t.get('repo','artdrop-protocol')}` | {owners} | {deps} |")
    with open(ROADMAP, "w") as f:
        f.write("\n".join(lines) + "\n")
    print(f"wrote {ROADMAP}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", required=True)
    ap.add_argument("--roadmap-only", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    a = ap.parse_args()
    data = load()
    write_roadmap(data)
    if a.roadmap_only:
        return
    home = short(a.repo)
    home_tasks = [t for t in data["tasks"] if trepo(t) == home]
    phases = {k: v for k, v in data["phases"].items() if any(t["phase"] == k for t in home_tasks)}
    ensure_milestones(a.repo, phases, a.dry_run)
    ensure_labels(a.repo, home_tasks, a.dry_run)
    sync_issues(a.repo, data, a.dry_run)


if __name__ == "__main__":
    main()
