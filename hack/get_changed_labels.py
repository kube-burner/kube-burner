#!/usr/bin/env python3
"""
Script to parse bats_test_mappings.yml and return labels for paths that have been updated in git.
"""

import os
import subprocess
import sys
import yaml
from fnmatch import fnmatch


def get_changed_files(repo_root):
    """Get list of all changed files (staged and unstaged) from git."""
    changed_files = set()
    
    try:
        # Get unstaged changes
        result = subprocess.run(
            ["git", "diff", "--name-only", "origin/main"],
            cwd=repo_root,
            capture_output=True,
            text=True,
            check=False
        )
        if result.returncode == 0:
            changed_files.update(line.strip() for line in result.stdout.splitlines() if line.strip())
        else:
            raise Exception(f"Failed to get unstaged changes: {result.stderr}")
    except Exception as e:
        print(f"Warning: Error getting changed files: {e}", file=sys.stderr)
    
    return sorted(changed_files)


def matches_pattern(file_path, pattern):
    """
    Check if a file path matches a glob pattern.
    Patterns are relative to pkg/ directory and may contain wildcards.
    """
    # Paths in YAML are relative to pkg/, so prepend pkg/ to the pattern
    full_pattern = f"{pattern}"
    
    # Direct match
    if file_path == full_pattern:
        return True
    
    # Use fnmatch for glob-style matching
    if fnmatch(file_path, full_pattern):
        return True
    
    # For directory patterns ending with /*, check if file is in that directory
    if full_pattern.endswith("/*"):
        dir_pattern = full_pattern[:-2]  # Remove /*
        if file_path.startswith(dir_pattern + "/"):
            return True
    
    return False


def check_paths_changed(paths, changed_files):
    """Check if any files matching the given paths have been changed."""
    if not changed_files:
        return False
    
    for path_pattern in paths:
        for changed_file in changed_files:
            if matches_pattern(changed_file, path_pattern):
                return True
    
    return False


def main():
    if len(sys.argv) != 2:
        print("Usage: get_changed_labels.py <mappings_file>", file=sys.stderr)
        sys.exit(1)
    mappings_file = sys.argv[1]
    if not os.path.exists(mappings_file):
        print(f"Error: {mappings_file} not found", file=sys.stderr)
        sys.exit(1)
    
    # Get changed files
    changed_files = get_changed_files(os.getcwd())
    
    try:
        with open(mappings_file, "r") as f:
            data = yaml.safe_load(f)
    except Exception as e:
        print(f"Error: Failed to parse YAML file: {e}", file=sys.stderr)
        sys.exit(1)
    
    # Process each label/paths entry
    labels_to_output = []
    for entry in data:
        if "label" in entry and "paths" in entry:
            if not isinstance(entry["paths"], list):
                print(f"Invalid YAML structure, paths should be a list", file=sys.stderr)
                sys.exit(1)
            
            label = entry["label"]
            paths = entry["paths"]

            if check_paths_changed(paths, changed_files):
                labels_to_output.append(label)
        else:
            print(f"Invalid YAML structure, either label or paths are missing", file=sys.stderr)
            sys.exit(1)
    
    # Output unique labels (one per line)
    if labels_to_output:
        unique_labels = sorted(set(labels_to_output))
        for label in unique_labels:
            print(f"--filter-tags {label}", end=" ")


if __name__ == "__main__":
    main()

