This document outlines how to set up and execute releases on GitHub for the loft-sh/cluster-api-provider-vcluster repository using the provided GitHub Actions workflow file. The workflow is designed to automate the release process, including setting up the environment, preparing release files, publishing images, and creating or editing a release.

# Workflow Overview

The GitHub Actions workflow is activated when a release is created in GitHub. It includes jobs to prepare release files, build and push Docker images, and handle the release setup. The workflow is mainly triggered by the presence of a new tag that starts with 'v'.

# Usage Instructions

# Preparing and Triggering a Release
1. Finalize Your Changes:
Ensure that all changes intended for the next release are merged into the main branch.

2. Create a Tag:
Tag your release using semantic versioning, prefixed with v. For example, v1.0.0.

3. Draft a Release:
- Go to the Releases section of your GitHub repository.
- Click on "Draft a new release".
- Enter the tag you created.
- Set the release as "Set as a pre-release" initially.
- Add release notes detailing the changes, features, or fixes.

4. Publish the Draft Release:
- Once the draft is ready, publish it.
- This action will trigger the GitHub Actions workflow.

5. Monitor the Workflow:
- Observe the progress in the Actions tab.
- Ensure that all steps complete successfully.

6. Finalizing the Release:
- After the workflow completes and optional validation has been done, you can adjust the release settings.
- Change it from "Set as a pre-release" to "Set as the latest release".

# Handling Workflow Failures
If the workflow fails:

- Check the GitHub Actions logs for specific errors.
- Fix issues in the workflow file if necessary.
- For non-alpha/beta releases, consider deleting and recreating the tag/release to retrigger the workflow.
- For alpha/beta releases, create the next build version to address the issues.