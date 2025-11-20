# gh-review-kit

A tool to manage GitHub reviews.

## Installation

```sh
gh extension install srz-zumix/gh-review-kit
```

## Commands

### Re-request review for a pull request

```sh
gh review-kit rerequest <pull-request-number> [--repo REPO] [--reviewers REVIEWERS] [--exclude-approved]
```

Re-request review for a pull request.

If reviewers are not specified, the command will re-request review from all reviewers who have already submitted a review.
If reviewers are specified, the command will re-request review from the specified reviewers only.

**Options:**

- `--exclude-approved`: Exclude reviewers who have already approved (optional)
- `--repo, -R`: Repository in the format 'owner/repo' (optional, defaults to current repository)
- `--reviewers, -r`: Reviewers to re-request (optional, users or teams, e.g., username or org/team)

**Examples:**

```sh
# Re-request review from all reviewers who have already reviewed
gh review-kit rerequest 123

# Re-request review from reviewers excluding those who approved
gh review-kit rerequest 123 --exclude-approved

# Re-request review from specific reviewers
gh review-kit rerequest 123 --reviewers user1,user2,@org/team

# Re-request review from specific reviewers, excluding those who approved
gh review-kit rerequest 123 --reviewers user1,user2,user3 --exclude-approved

# Re-request review in a different repository
gh review-kit rerequest 123 --repo owner-name/repo-name
```
