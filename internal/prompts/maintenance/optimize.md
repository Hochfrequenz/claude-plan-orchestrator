---
id: optimize
name: Optimize Performance
description: Identify and fix performance bottlenecks
scopes: [module, package, all]
---
You are performing a performance optimization task on {{.Scope}}.

Your goal is to identify and fix performance issues. Focus on:

1. Reduce unnecessary allocations
2. Optimize hot paths and loops
3. Add caching where appropriate
4. Reduce database/network round trips
5. Use more efficient data structures
6. Parallelize independent operations where safe

**Important:**
- Profile before optimizing - focus on actual bottlenecks
- Document performance improvements with benchmarks if possible
- Don't sacrifice readability for micro-optimizations
- Run tests after changes to verify correctness
- Create a PR with your changes when done

Start by identifying the most impactful optimization opportunities.
