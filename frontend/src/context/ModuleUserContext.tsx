import { createContext, useContext } from 'react';
import type { CurrentUserProfile } from '../types';

export interface ModuleUserContextValue {
  user: CurrentUserProfile | null;
  loadingUser: boolean;
  isEmbedded: boolean;
  reloadUser: () => Promise<void>;
}

const ModuleUserContext = createContext<ModuleUserContextValue>({
  user: null,
  loadingUser: false,
  isEmbedded: false,
  reloadUser: async () => {},
});

export const ModuleUserProvider = ModuleUserContext.Provider;

export function useModuleUser(): ModuleUserContextValue {
  return useContext(ModuleUserContext);
}
