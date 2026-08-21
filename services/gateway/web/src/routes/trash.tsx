import { createFileRoute } from '@tanstack/react-router';

import { TrashPage } from '@/components/TrashPage';

export const Route = createFileRoute('/trash')({
  component: TrashPage,
});
